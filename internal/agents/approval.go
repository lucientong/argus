package agents

import (
	"context"
	"fmt"

	wagentpkg "github.com/lucientong/waggle/pkg/agent"

	"github.com/lucientong/argus/internal/integrations/slack"
	"github.com/lucientong/argus/internal/types"
)

// NewApprovalAgent returns an Agent[types.RemediationPlan, types.ApprovalDecision].
//
// Behaviour:
//   - If plan.RequiresApproval == false, the plan is auto-approved without contacting Slack.
//   - If plan.RequiresApproval == true, a blocking approval request is sent to the configured
//     Slack channel. The agent waits until the human approves or denies.
//
// channel is the Slack channel ID / name to post the approval request to.
func NewApprovalAgent(slackClient slack.Client, channel string) wagentpkg.Agent[types.RemediationPlan, types.ApprovalDecision] {
	return wagentpkg.Func[types.RemediationPlan, types.ApprovalDecision](
		"approval-gate",
		func(ctx context.Context, plan types.RemediationPlan) (types.ApprovalDecision, error) {
			// Auto-approve when no high-risk actions are present.
			if !plan.RequiresApproval {
				return types.ApprovalDecision{
					Plan:     plan,
					Approved: true,
					Comment:  "auto-approved (no high-risk actions)",
				}, nil
			}

			// Build a human-readable summary for the Slack message.
			text := buildApprovalText(plan)
			callbackID := fmt.Sprintf("argus-approval-%s", plan.Diagnosis.Alert.Alert.ID)

			resp, err := slackClient.RequestApproval(ctx, slack.ApprovalRequest{
				Channel:    channel,
				Text:       text,
				CallbackID: callbackID,
			})
			if err != nil {
				return types.ApprovalDecision{}, fmt.Errorf("approval gate: slack request failed: %w", err)
			}

			return types.ApprovalDecision{
				Plan:     plan,
				Approved: resp.Approved,
				Comment:  resp.Comment,
			}, nil
		},
	)
}

// buildApprovalText formats a RemediationPlan into a human-readable Slack message.
func buildApprovalText(plan types.RemediationPlan) string {
	diag := plan.Diagnosis
	msg := fmt.Sprintf(
		":rotating_light: *Argus Approval Required*\n\n"+
			"*Alert:* %s\n"+
			"*Service:* %s\n"+
			"*Hypothesis:* %s\n\n"+
			"*Runbook:* %s\n\n"+
			"*Proposed Actions:*\n",
		diag.Alert.Alert.Title,
		diag.Alert.Alert.Service,
		diag.Hypothesis,
		plan.Runbook.Title,
	)
	for i, a := range plan.Actions {
		msg += fmt.Sprintf("%d. [%s] %s\n   `%s`\n", i+1, a.RiskLevel, a.Description, a.Command)
	}
	msg += fmt.Sprintf("\n*Rationale:* %s\n\nPlease approve or deny.", plan.Rationale)
	return msg
}
