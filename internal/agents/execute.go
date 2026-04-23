package agents

import (
	"context"
	"fmt"
	"strings"

	wagentpkg "github.com/lucientong/waggle/pkg/agent"
	"github.com/lucientong/waggle/pkg/guardrail"

	"github.com/lucientong/argus/internal/types"
)

// Executor runs a shell command and returns its output.
// The real implementation uses os/exec; tests inject a mock.
type Executor interface {
	Execute(ctx context.Context, command string) (string, error)
}

// NewExecuteAgent returns an Agent[types.ApprovalDecision, types.ExecutionResult].
//
// Behaviour:
//   - If the plan was not approved, returns a failed ExecutionResult immediately.
//   - Each action command is validated against dangerousCommandGuard before execution.
//   - Actions are executed sequentially; execution stops on first failure.
func NewExecuteAgent(exec Executor) wagentpkg.Agent[types.ApprovalDecision, types.ExecutionResult] {
	// Build a command-level guardrail using WithInputExtractGuard.
	// The inner agent executes a single RemediationAction; we wrap it to guard the command string.
	innerExec := wagentpkg.Func[types.RemediationAction, types.ActionOutcome](
		"action-executor",
		func(ctx context.Context, action types.RemediationAction) (types.ActionOutcome, error) {
			out, err := exec.Execute(ctx, action.Command)
			if err != nil {
				return types.ActionOutcome{
					Action:  action,
					Output:  out,
					Success: false,
					Error:   err.Error(),
				}, nil // non-fatal: record outcome but don't abort the agent chain
			}
			return types.ActionOutcome{
				Action:  action,
				Output:  out,
				Success: true,
			}, nil
		},
	)

	guardedExec := guardrail.WithInputExtractGuard(
		innerExec,
		func(a types.RemediationAction) string { return a.Command },
		dangerousCommandGuard(),
	)

	return wagentpkg.Func[types.ApprovalDecision, types.ExecutionResult](
		"execute",
		func(ctx context.Context, decision types.ApprovalDecision) (types.ExecutionResult, error) {
			if !decision.Approved {
				return types.ExecutionResult{
					Plan:    decision.Plan,
					Success: false,
					Error:   fmt.Sprintf("execution skipped: plan not approved (comment: %s)", decision.Comment),
				}, nil
			}

			outcomes := make([]types.ActionOutcome, 0, len(decision.Plan.Actions))
			overallSuccess := true

			for _, action := range decision.Plan.Actions {
				outcome, err := guardedExec.Run(ctx, action)
				if err != nil {
					// Guard violation or unexpected error — record and stop.
					outcomes = append(outcomes, types.ActionOutcome{
						Action:  action,
						Success: false,
						Error:   err.Error(),
					})
					overallSuccess = false
					break
				}
				outcomes = append(outcomes, outcome)
				if !outcome.Success {
					overallSuccess = false
					break // stop on first failed action
				}
			}

			return types.ExecutionResult{
				Plan:    decision.Plan,
				Actions: outcomes,
				Success: overallSuccess,
			}, nil
		},
	)
}

// dangerousCommandGuard returns a Validator that rejects obviously destructive commands.
// It is intentionally conservative: allow kubectl/helm/psql but block nuclear operations.
func dangerousCommandGuard() guardrail.Validator {
	forbidden := []string{
		"kubectl delete namespace",
		"kubectl delete node",
		"rm -rf /",
		"rm -rf /*",
		"DROP DATABASE",
		"DROP TABLE",
		"TRUNCATE",
		"format ",
		"mkfs",
		"> /dev/sda",
	}
	lower := make([]string, len(forbidden))
	for i, f := range forbidden {
		lower[i] = strings.ToLower(f)
	}
	return guardrail.NewValidator("dangerous-command", func(cmd string) error {
		cmdLower := strings.ToLower(cmd)
		for _, f := range lower {
			if strings.Contains(cmdLower, f) {
				return fmt.Errorf("command contains dangerous pattern %q — blocked by safety guardrail", f)
			}
		}
		return nil
	})
}
