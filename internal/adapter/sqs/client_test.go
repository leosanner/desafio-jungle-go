package sqsadapter

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

type fakeQueueAPI struct {
	urls map[string]error
	seen []string
}

func (f *fakeQueueAPI) GetQueueUrl(_ context.Context, params *sqs.GetQueueUrlInput, _ ...func(*sqs.Options)) (*sqs.GetQueueUrlOutput, error) {
	name := ""
	if params.QueueName != nil {
		name = *params.QueueName
	}
	f.seen = append(f.seen, name)
	if err, ok := f.urls[name]; ok {
		if err != nil {
			return nil, err
		}
		return &sqs.GetQueueUrlOutput{}, nil
	}
	return nil, errors.New("unknown queue")
}

func TestCheckQueuesBothExist(t *testing.T) {
	t.Parallel()
	api := &fakeQueueAPI{urls: map[string]error{
		"wager.fifo": nil,
		"dlq.fifo":   nil,
	}}
	c := &Client{api: api, wager: "wager.fifo", dlq: "dlq.fifo"}
	if err := c.CheckQueues(context.Background()); err != nil {
		t.Fatalf("CheckQueues: %v", err)
	}
	if len(api.seen) != 2 {
		t.Fatalf("GetQueueUrl calls = %d, want 2", len(api.seen))
	}
}

func TestCheckQueuesMissingWager(t *testing.T) {
	t.Parallel()
	api := &fakeQueueAPI{urls: map[string]error{
		"wager.fifo": errors.New("not found"),
		"dlq.fifo":   nil,
	}}
	c := &Client{api: api, wager: "wager.fifo", dlq: "dlq.fifo"}
	err := c.CheckQueues(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); !strings.Contains(got, "wager.fifo") {
		t.Errorf("error %q should mention wager queue", got)
	}
}

func TestCheckQueuesMissingDLQ(t *testing.T) {
	t.Parallel()
	api := &fakeQueueAPI{urls: map[string]error{
		"wager.fifo": nil,
		"dlq.fifo":   errors.New("not found"),
	}}
	c := &Client{api: api, wager: "wager.fifo", dlq: "dlq.fifo"}
	err := c.CheckQueues(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); !strings.Contains(got, "dlq.fifo") {
		t.Errorf("error %q should mention dlq", got)
	}
}
