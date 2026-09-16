package sqsadapter

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/leosanner/desafio-jungle-go/internal/app"
)

// InboundMessage is one received wager-queue message.
type InboundMessage struct {
	ReceiptHandle string
	Body          string
	ReceiveCount  int
	GroupID       string
}

func (c *Client) namedQueueURL(ctx context.Context, name string, cached *string) (string, error) {
	c.mu.Lock()
	url := *cached
	c.mu.Unlock()
	if url != "" {
		return url, nil
	}
	got, err := c.queueURL(ctx, name)
	if err != nil {
		return "", fmt.Errorf("sqs: queue %q: %w", name, err)
	}
	c.mu.Lock()
	*cached = got
	c.mu.Unlock()
	return got, nil
}

func (c *Client) wagerQueueURL(ctx context.Context) (string, error) {
	return c.namedQueueURL(ctx, c.wager, &c.wagerURL)
}

func (c *Client) dlqQueueURL(ctx context.Context) (string, error) {
	return c.namedQueueURL(ctx, c.dlq, &c.dlqURL)
}

// ReceiveWager long-polls the inbound wager queue.
func (c *Client) ReceiveWager(ctx context.Context, wait, visibility time.Duration) ([]InboundMessage, error) {
	url, err := c.wagerQueueURL(ctx)
	if err != nil {
		return nil, err
	}
	out, err := c.api.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            aws.String(url),
		MaxNumberOfMessages: 1,
		WaitTimeSeconds:     durationSeconds(wait),
		VisibilityTimeout:   durationSeconds(visibility),
		MessageSystemAttributeNames: []types.MessageSystemAttributeName{
			types.MessageSystemAttributeNameApproximateReceiveCount,
			types.MessageSystemAttributeNameMessageGroupId,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("sqs: receive wager: %w", err)
	}
	msgs := make([]InboundMessage, 0, len(out.Messages))
	for _, m := range out.Messages {
		msgs = append(msgs, inboundFrom(m))
	}
	return msgs, nil
}

func inboundFrom(m types.Message) InboundMessage {
	msg := InboundMessage{ReceiveCount: 1}
	if m.ReceiptHandle != nil {
		msg.ReceiptHandle = *m.ReceiptHandle
	}
	if m.Body != nil {
		msg.Body = *m.Body
	}
	if m.Attributes != nil {
		if n, err := strconv.Atoi(m.Attributes[string(types.MessageSystemAttributeNameApproximateReceiveCount)]); err == nil && n > 0 {
			msg.ReceiveCount = n
		}
		msg.GroupID = m.Attributes[string(types.MessageSystemAttributeNameMessageGroupId)]
	}
	return msg
}

func durationSeconds(d time.Duration) int32 {
	if d <= 0 {
		return 0
	}
	s := int32(d / time.Second)
	if s < 1 && d > 0 {
		return 1
	}
	return s
}

// Delete removes a message from the wager queue (after a durable commit).
func (c *Client) Delete(ctx context.Context, receiptHandle string) error {
	url, err := c.wagerQueueURL(ctx)
	if err != nil {
		return err
	}
	_, err = c.api.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(url),
		ReceiptHandle: aws.String(receiptHandle),
	})
	if err != nil {
		return fmt.Errorf("sqs: delete: %w", err)
	}
	return nil
}

// ChangeVisibility sets the remaining visibility timeout for an in-flight message.
func (c *Client) ChangeVisibility(ctx context.Context, receiptHandle string, timeout time.Duration) error {
	url, err := c.wagerQueueURL(ctx)
	if err != nil {
		return err
	}
	_, err = c.api.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{
		QueueUrl:          aws.String(url),
		ReceiptHandle:     aws.String(receiptHandle),
		VisibilityTimeout: durationSeconds(timeout),
	})
	if err != nil {
		return fmt.Errorf("sqs: change visibility: %w", err)
	}
	return nil
}

// SendToDLQ copies a body onto the configured dead-letter queue.
func (c *Client) SendToDLQ(ctx context.Context, body, groupID, dedupID string) error {
	url, err := c.dlqQueueURL(ctx)
	if err != nil {
		return err
	}
	if groupID == "" {
		groupID = "invalid"
	}
	if dedupID == "" {
		dedupID = app.HashMessageBody([]byte(body))
	}
	_, err = c.api.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:               aws.String(url),
		MessageBody:            aws.String(body),
		MessageGroupId:         aws.String(groupID),
		MessageDeduplicationId: aws.String(dedupID),
	})
	if err != nil {
		return fmt.Errorf("sqs: send dlq: %w", err)
	}
	return nil
}

// SendWager publishes an inbound FIFO message (tests and operators).
func (c *Client) SendWager(ctx context.Context, body, groupID, dedupID string) error {
	url, err := c.wagerQueueURL(ctx)
	if err != nil {
		return err
	}
	_, err = c.api.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:               aws.String(url),
		MessageBody:            aws.String(body),
		MessageGroupId:         aws.String(groupID),
		MessageDeduplicationId: aws.String(dedupID),
	})
	if err != nil {
		return fmt.Errorf("sqs: send wager: %w", err)
	}
	return nil
}

// ReceiveDLQ long-polls the dead-letter queue (tests).
func (c *Client) ReceiveDLQ(ctx context.Context, max int32, waitSeconds int32) ([]string, error) {
	url, err := c.dlqQueueURL(ctx)
	if err != nil {
		return nil, err
	}
	out, err := c.api.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            aws.String(url),
		MaxNumberOfMessages: max,
		WaitTimeSeconds:     waitSeconds,
		VisibilityTimeout:   5,
	})
	if err != nil {
		return nil, fmt.Errorf("sqs: receive dlq: %w", err)
	}
	bodies := make([]string, 0, len(out.Messages))
	for _, m := range out.Messages {
		if m.Body != nil {
			bodies = append(bodies, *m.Body)
		}
	}
	return bodies, nil
}

// CreateFIFOQueueWithRedrive provisions a FIFO pair for inbound tests.
func (c *Client) CreateFIFOQueueWithRedrive(ctx context.Context, mainName, dlqName string, visibility time.Duration, maxReceive int) error {
	if err := c.CreateFIFOQueue(ctx, dlqName); err != nil {
		return err
	}
	dlqURL, err := c.queueURL(ctx, dlqName)
	if err != nil {
		return err
	}
	attrs, err := c.api.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(dlqURL),
		AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameQueueArn},
	})
	if err != nil {
		return fmt.Errorf("sqs: dlq arn: %w", err)
	}
	arn := ""
	if attrs.Attributes != nil {
		arn = attrs.Attributes[string(types.QueueAttributeNameQueueArn)]
	}
	if arn == "" {
		return fmt.Errorf("sqs: empty dlq arn")
	}
	_, err = c.api.CreateQueue(ctx, &sqs.CreateQueueInput{
		QueueName: aws.String(mainName),
		Attributes: map[string]string{
			"FifoQueue":                 "true",
			"ContentBasedDeduplication": "false",
			"VisibilityTimeout":         strconv.Itoa(int(durationSeconds(visibility))),
			"RedrivePolicy":             fmt.Sprintf(`{"deadLetterTargetArn":"%s","maxReceiveCount":"%d"}`, arn, maxReceive),
		},
	})
	if err != nil {
		return fmt.Errorf("sqs: create queue %q: %w", mainName, err)
	}
	return nil
}
