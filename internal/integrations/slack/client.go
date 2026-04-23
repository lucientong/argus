// Package slack defines the interface for posting messages to Slack.
package slack

import "context"

// Message is a Slack message to post.
type Message struct {
	Channel string
	Text    string
	// Blocks is an optional Slack Block Kit payload (JSON string).
	Blocks string
}

// ApprovalRequest is a message sent to Slack requesting human approval.
type ApprovalRequest struct {
	Channel     string
	Text        string
	CallbackID  string // used to correlate the response
}

// ApprovalResponse is the human's answer to an ApprovalRequest.
type ApprovalResponse struct {
	CallbackID string
	Approved   bool
	Comment    string
	Approver   string
}

// Client is the interface for sending messages and handling approvals via Slack.
type Client interface {
	// PostMessage sends a message to the specified channel.
	PostMessage(ctx context.Context, msg Message) error

	// RequestApproval posts an approval request and waits (blocking) for the human response.
	// The implementation must handle the Slack interactive callback internally.
	RequestApproval(ctx context.Context, req ApprovalRequest) (ApprovalResponse, error)
}

// MockClient records sent messages for assertions in tests.
// Approval always returns the value configured in AutoApprove.
type MockClient struct {
	Sent        []Message
	Approvals   []ApprovalRequest
	AutoApprove bool
	ApproveComment string
	Err         error
}

func (m *MockClient) PostMessage(_ context.Context, msg Message) error {
	if m.Err != nil {
		return m.Err
	}
	m.Sent = append(m.Sent, msg)
	return nil
}

func (m *MockClient) RequestApproval(_ context.Context, req ApprovalRequest) (ApprovalResponse, error) {
	if m.Err != nil {
		return ApprovalResponse{}, m.Err
	}
	m.Approvals = append(m.Approvals, req)
	return ApprovalResponse{
		CallbackID: req.CallbackID,
		Approved:   m.AutoApprove,
		Comment:    m.ApproveComment,
		Approver:   "mock-user",
	}, nil
}
