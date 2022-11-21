package asyncjob

import (
	"context"
	"fmt"
	"time"

	"github.com/Azure/go-asynctask"
)

// StepFromJobInput: steps that consumes job input
func StepFromJobInput[JT, ST any](bCtx context.Context, j *JobDefinition[JT], stepName string, stepFunc asynctask.ContinueFunc[JT, ST], optionDecorators ...ExecutionOptionPreparer) (*StepDefinition[ST], error) {
	return StepAfter[JT, ST](bCtx, j, stepName, j.rootStep, stepFunc, optionDecorators...)
}

// AddStep: add a step without take input
func AddStep[ST any](bCtx context.Context, j JobDefinitionMeta, stepName string, stepFunc asynctask.AsyncFunc[ST], optionDecorators ...ExecutionOptionPreparer) (*StepDefinition[ST], error) {
	stepD := newStepDefinition[ST](stepName, stepTypeTask, optionDecorators...)
	precedingDefSteps, err := getDependsOnSteps(stepD, j)
	if err != nil {
		return nil, err
	}

	// if a step have no preceding tasks, link it to our rootJob as preceding task, so it won't start yet.
	if len(precedingDefSteps) == 0 {
		precedingDefSteps = append(precedingDefSteps, j.RootStep())
		stepD.executionOptions.DependOn = append(stepD.executionOptions.DependOn, j.RootStep().GetName())
	}

	stepD.instanceCreator = func(ctx context.Context, ji JobInstanceMeta) StepInstanceMeta {
		// error is ignored here
		precedingInstances, precedingTasks, _ := getDependsOnStepInstances(stepD, ji)

		stepInstance := newStepInstance[ST](stepD)
		instrumentedFunc := func(ctx context.Context) (*ST, error) {
			if err := asynctask.WaitAll(ctx, &asynctask.WaitAllOptions{}, precedingTasks...); err != nil {
				/* this only work on ExecuteAfter from input, asynctask.ContinueWith and asynctask.AfterBoth won't invoke instrumentedFunc if any of the preceding task failed.
				   we need to be consistent on how to set state of dependent step.
				step.executionData.StartTime = time.Now()
				step.state = StepStateFailed
				step.executionData.Duration = 0 */
				return nil, newJobError(ErrPrecedentStepFailure, "")
			}

			stepInstance.executionData.StartTime = time.Now()
			stepInstance.state = StepStateRunning

			var result *ST
			var err error
			if stepD.executionOptions.RetryPolicy != nil {
				stepInstance.executionData.Retried = &RetryReport{}
				result, err = newRetryer(stepD.executionOptions.RetryPolicy, stepInstance.executionData.Retried, func() (*ST, error) { return stepFunc(ctx) }).Run()
			} else {
				result, err = stepFunc(ctx)
			}

			stepInstance.executionData.Duration = time.Since(stepInstance.executionData.StartTime)

			if err != nil {
				stepInstance.state = StepStateFailed
				return nil, newStepError(stepName, err)
			} else {
				stepInstance.state = StepStateCompleted
				return result, nil
			}
		}
		stepInstance.task = asynctask.Start(ctx, instrumentedFunc)
		ji.AddStepInstance(stepInstance, precedingInstances...)
		return stepInstance
	}

	j.AddStep(stepD, precedingDefSteps...)
	return stepD, nil
}

// StepAfter: add a step after a preceding step, also take input from that preceding step
func StepAfter[T, S any](bCtx context.Context, j JobDefinitionMeta, stepName string, parentStep *StepDefinition[T], stepFunc asynctask.ContinueFunc[T, S], optionDecorators ...ExecutionOptionPreparer) (*StepDefinition[S], error) {
	// check parentStepT is in this job
	if get, ok := j.GetStep(parentStep.GetName()); !ok || get != parentStep {
		return nil, fmt.Errorf("step [%s] not found in job", parentStep.GetName())
	}

	stepD := newStepDefinition[S](stepName, stepTypeTask, append(optionDecorators, ExecuteAfter(parentStep))...)
	precedingDefSteps, err := getDependsOnSteps(stepD, j)
	if err != nil {
		return nil, err
	}

	// if a step have no preceding tasks, link it to our rootJob as preceding task, so it won't start yet.
	if len(precedingDefSteps) == 0 {
		precedingDefSteps = append(precedingDefSteps, j.RootStep())
	}

	stepD.instanceCreator = func(ctx context.Context, ji JobInstanceMeta) StepInstanceMeta {
		// error is ignored here
		precedingInstances, precedingTasks, _ := getDependsOnStepInstances(stepD, ji)

		parentStepInstanceMeta, _ := ji.GetStepInstance(parentStep.GetName())
		var parentStepInstance *StepInstance[T] = parentStepInstanceMeta.(*StepInstance[T])

		stepInstance := newStepInstance[S](stepD)
		instrumentedFunc := func(ctx context.Context, t *T) (*S, error) {

			if err := asynctask.WaitAll(ctx, &asynctask.WaitAllOptions{}, precedingTasks...); err != nil {
				/* this only work on ExecuteAfter from input, asynctask.ContinueWith and asynctask.AfterBoth won't invoke instrumentedFunc if any of the preceding task failed.
				   we need to be consistent on how to set state of dependent step.
				step.executionData.StartTime = time.Now()
				step.state = StepStateFailed
				step.executionData.Duration = 0 */
				return nil, newJobError(ErrPrecedentStepFailure, "")
			}

			stepInstance.executionData.StartTime = time.Now()
			stepInstance.state = StepStateRunning

			var result *S
			var err error
			if stepD.executionOptions.RetryPolicy != nil {
				stepInstance.executionData.Retried = &RetryReport{}
				result, err = newRetryer(stepD.executionOptions.RetryPolicy, stepInstance.executionData.Retried, func() (*S, error) { return stepFunc(ctx, t) }).Run()
			} else {
				result, err = stepFunc(ctx, t)
			}

			stepInstance.executionData.Duration = time.Since(stepInstance.executionData.StartTime)

			if err != nil {
				stepInstance.state = StepStateFailed
				return nil, newStepError(stepName, err)
			} else {
				stepInstance.state = StepStateCompleted
				return result, nil
			}
		}
		stepInstance.task = asynctask.ContinueWith(ctx, parentStepInstance.task, instrumentedFunc)
		ji.AddStepInstance(stepInstance, precedingInstances...)
		return stepInstance
	}

	j.AddStep(stepD, precedingDefSteps...)
	return stepD, nil
}

// StepAfterBoth: add a step after both preceding steps, also take input from both preceding steps
func StepAfterBoth[T, S, R any](bCtx context.Context, j JobDefinitionMeta, stepName string, parentStepT *StepDefinition[T], parentStepS *StepDefinition[S], stepFunc asynctask.AfterBothFunc[T, S, R], optionDecorators ...ExecutionOptionPreparer) (*StepDefinition[R], error) {
	// check parentStepT is in this job
	if get, ok := j.GetStep(parentStepT.GetName()); !ok || get != parentStepT {
		return nil, fmt.Errorf("step [%s] not found in job", parentStepT.GetName())
	}
	if get, ok := j.GetStep(parentStepS.GetName()); !ok || get != parentStepS {
		return nil, fmt.Errorf("step [%s] not found in job", parentStepS.GetName())
	}

	stepD := newStepDefinition[R](stepName, stepTypeTask, append(optionDecorators, ExecuteAfter(parentStepT), ExecuteAfter(parentStepS))...)
	precedingDefSteps, err := getDependsOnSteps(stepD, j)
	if err != nil {
		return nil, err
	}

	// if a step have no preceding tasks, link it to our rootJob as preceding task, so it won't start yet.
	if len(precedingDefSteps) == 0 {
		precedingDefSteps = append(precedingDefSteps, j.RootStep())
	}

	stepD.instanceCreator = func(ctx context.Context, ji JobInstanceMeta) StepInstanceMeta {
		// error is ignored here
		precedingInstances, precedingTasks, _ := getDependsOnStepInstances(stepD, ji)

		parentStepTInstanceMeta, _ := ji.GetStepInstance(parentStepT.GetName())
		var parentStepTInstance *StepInstance[T] = parentStepTInstanceMeta.(*StepInstance[T])

		parentStepSInstanceMeta, _ := ji.GetStepInstance(parentStepS.GetName())
		var parentStepSInstance *StepInstance[S] = parentStepSInstanceMeta.(*StepInstance[S])

		stepInstance := newStepInstance[R](stepD)
		instrumentedFunc := func(ctx context.Context, t *T, s *S) (*R, error) {

			if err := asynctask.WaitAll(ctx, &asynctask.WaitAllOptions{}, precedingTasks...); err != nil {
				/* this only work on ExecuteAfter from input, asynctask.ContinueWith and asynctask.AfterBoth won't invoke instrumentedFunc if any of the preceding task failed.
				   we need to be consistent on how to set state of dependent step.
				step.executionData.StartTime = time.Now()
				step.state = StepStateFailed
				step.executionData.Duration = 0 */
				return nil, newJobError(ErrPrecedentStepFailure, "")
			}

			stepInstance.executionData.StartTime = time.Now()
			stepInstance.state = StepStateRunning

			var result *R
			var err error
			if stepD.executionOptions.RetryPolicy != nil {
				stepInstance.executionData.Retried = &RetryReport{}
				result, err = newRetryer(stepD.executionOptions.RetryPolicy, stepInstance.executionData.Retried, func() (*R, error) { return stepFunc(ctx, t, s) }).Run()
			} else {
				result, err = stepFunc(ctx, t, s)
			}

			stepInstance.executionData.Duration = time.Since(stepInstance.executionData.StartTime)

			if err != nil {
				stepInstance.state = StepStateFailed
				return nil, newStepError(stepName, err)
			} else {
				stepInstance.state = StepStateCompleted
				return result, nil
			}
		}
		stepInstance.task = asynctask.AfterBoth(ctx, parentStepTInstance.task, parentStepSInstance.task, instrumentedFunc)
		ji.AddStepInstance(stepInstance, precedingInstances...)
		return stepInstance
	}

	j.AddStep(stepD, precedingDefSteps...)
	return stepD, nil
}

func getDependsOnSteps(step StepDefinitionMeta, j JobDefinitionMeta) ([]StepDefinitionMeta, error) {
	var precedingDefSteps []StepDefinitionMeta
	for _, depStepName := range step.DependsOn() {
		if depStep, ok := j.GetStep(depStepName); ok {
			precedingDefSteps = append(precedingDefSteps, depStep)
		} else {
			return nil, fmt.Errorf("step [%s] not found", depStepName)
		}
	}

	return precedingDefSteps, nil
}

func getDependsOnStepInstances(stepD StepDefinitionMeta, ji JobInstanceMeta) ([]StepInstanceMeta, []asynctask.Waitable, error) {
	var precedingInstances []StepInstanceMeta
	var precedingTasks []asynctask.Waitable
	for _, depStepName := range stepD.DependsOn() {
		if depStep, ok := ji.GetStepInstance(depStepName); ok {
			precedingInstances = append(precedingInstances, depStep)
			precedingTasks = append(precedingTasks, depStep.Waitable())
		} else {
			return nil, nil, fmt.Errorf("runtime step [%s] not found", depStepName)
		}
	}

	return precedingInstances, precedingTasks, nil
}
