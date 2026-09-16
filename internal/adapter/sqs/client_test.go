package sqsadapter

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"github.com/leosanner/desafio-jungle-go/internal/app"
)

type fakeQueueAPI struct {
	urls      map[string]error
	seen      []string
	sent      []*sqs.SendMessageInput
	sendErr   error
	queueURLs map[string]string
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
		url := "http://sqs/queue/" + name
		if f.queueURLs != nil {
			if u, ok := f.queueURLs[name]; ok {
				url = u
			}
		}
		return &sqs.GetQueueUrlOutput{QueueUrl: aws.String(url)}, nil
	}
	return nil, errors.New("unknown queue")
}

func (f *fakeQueueAPI) SendMessage(_ context.Context, params *sqs.SendMessageInput, _ ...func(*sqs.Options)) (*sqs.SendMessageOutput, error) {
	if f.sendErr != nil {
		return nil, f.sendErr
	}
	f.sent = append(f.sent, params)
	return &sqs.SendMessageOutput{}, nil
}

func (f *fakeQueueAPI) ReceiveMessage(context.Context, *sqs.ReceiveMessageInput, ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
	return &sqs.ReceiveMessageOutput{}, nil
}

func (f *fakeQueueAPI) CreateQueue(context.Context, *sqs.CreateQueueInput, ...func(*sqs.Options)) (*sqs.CreateQueueOutput, error) {
	return &sqs.CreateQueueOutput{}, nil
}

func (f *fakeQueueAPI) DeleteQueue(context.Context, *sqs.DeleteQueueInput, ...func(*sqs.Options)) (*sqs.DeleteQueueOutput, error) {
	return &sqs.DeleteQueueOutput{}, nil
}

func TestCheckQueuesBothExist(t *testing.T) {
	t.Parallel()
	api := &fakeQueueAPI{urls: map[string]error{
		"wager.fifo":  nil,
		"dlq.fifo":    nil,
		"events.fifo": nil,
	}}
	c := &Client{api: api, wager: "wager.fifo", dlq: "dlq.fifo", events: "events.fifo"}
	if err := c.CheckQueues(context.Background()); err != nil {
		t.Fatalf("CheckQueues: %v", err)
	}
	if len(api.seen) != 3 {
		t.Fatalf("GetQueueUrl calls = %d, want 3", len(api.seen))
	}
	if c.eventsURL == "" {
		t.Fatal("eventsURL not cached")
	}
}

func TestCheckQueuesMissingWager(t *testing.T) {
	t.Parallel()
	api := &fakeQueueAPI{urls: map[string]error{
		"wager.fifo":  errors.New("not found"),
		"dlq.fifo":    nil,
		"events.fifo": nil,
	}}
	c := &Client{api: api, wager: "wager.fifo", dlq: "dlq.fifo", events: "events.fifo"}
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
		"wager.fifo":  nil,
		"dlq.fifo":    errors.New("not found"),
		"events.fifo": nil,
	}}
	c := &Client{api: api, wager: "wager.fifo", dlq: "dlq.fifo", events: "events.fifo"}
	err := c.CheckQueues(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); !strings.Contains(got, "dlq.fifo") {
		t.Errorf("error %q should mention dlq", got)
	}
}

func TestCheckQueuesMissingEvents(t *testing.T) {
	t.Parallel()
	api := &fakeQueueAPI{urls: map[string]error{
		"wager.fifo":  nil,
		"dlq.fifo":    nil,
		"events.fifo": errors.New("not found"),
	}}
	c := &Client{api: api, wager: "wager.fifo", dlq: "dlq.fifo", events: "events.fifo"}
	err := c.CheckQueues(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); !strings.Contains(got, "events.fifo") {
		t.Errorf("error %q should mention events queue", got)
	}
}

func TestPublishSendsFIFOAttributes(t *testing.T) {
	t.Parallel()
	api := &fakeQueueAPI{urls: map[string]error{"events.fifo": nil}}
	c := &Client{api: api, events: "events.fifo"}
	env := app.Envelope{
		EventID:       "evt-1",
		EventType:     "WagerTransactionProcessed",
		AggregateID:   "tx-1",
		CorrelationID: "corr-1",
		Version:       1,
		Data:          []byte(`{"transactionId":"tx-1"}`),
	}
	if err := c.Publish(context.Background(), env); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(api.sent) != 1 {
		t.Fatalf("sent = %d, want 1", len(api.sent))
	}
	got := api.sent[0]
	if aws.ToString(got.MessageGroupId) != "tx-1" {
		t.Errorf("MessageGroupId = %q", aws.ToString(got.MessageGroupId))
	}
	if aws.ToString(got.MessageDeduplicationId) != "evt-1" {
		t.Errorf("MessageDeduplicationId = %q", aws.ToString(got.MessageDeduplicationId))
	}
	if !strings.Contains(aws.ToString(got.MessageBody), `"eventId":"evt-1"`) {
		t.Errorf("body = %s", aws.ToString(got.MessageBody))
	}
}
