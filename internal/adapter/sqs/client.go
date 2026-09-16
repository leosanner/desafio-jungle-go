package sqsadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"github.com/leosanner/desafio-jungle-go/internal/app"
	"github.com/leosanner/desafio-jungle-go/internal/config"
)

// Client is an SQS adapter used for readiness, outbox publish, and inbound consume.
type Client struct {
	api       queueAPI
	wager     string
	dlq       string
	events    string
	mu        sync.Mutex
	wagerURL  string
	dlqURL    string
	eventsURL string
}

type queueAPI interface {
	GetQueueUrl(ctx context.Context, params *sqs.GetQueueUrlInput, optFns ...func(*sqs.Options)) (*sqs.GetQueueUrlOutput, error)
	GetQueueAttributes(ctx context.Context, params *sqs.GetQueueAttributesInput, optFns ...func(*sqs.Options)) (*sqs.GetQueueAttributesOutput, error)
	SendMessage(ctx context.Context, params *sqs.SendMessageInput, optFns ...func(*sqs.Options)) (*sqs.SendMessageOutput, error)
	ReceiveMessage(ctx context.Context, params *sqs.ReceiveMessageInput, optFns ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error)
	DeleteMessage(ctx context.Context, params *sqs.DeleteMessageInput, optFns ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error)
	ChangeMessageVisibility(ctx context.Context, params *sqs.ChangeMessageVisibilityInput, optFns ...func(*sqs.Options)) (*sqs.ChangeMessageVisibilityOutput, error)
	CreateQueue(ctx context.Context, params *sqs.CreateQueueInput, optFns ...func(*sqs.Options)) (*sqs.CreateQueueOutput, error)
	DeleteQueue(ctx context.Context, params *sqs.DeleteQueueInput, optFns ...func(*sqs.Options)) (*sqs.DeleteQueueOutput, error)
}

// NewClient builds an AWS SDK v2 SQS client from Config.
// When AWS_ENDPOINT_URL is set, requests are sent to that endpoint (LocalStack).
func NewClient(cfg config.Config) (*Client, error) {
	ctx := context.Background()
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.AWSRegion),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AWSAccessKeyID,
			cfg.AWSSecretAccessKey,
			"",
		)),
		// Static keys + optional custom endpoint: never wait on EC2 IMDS (Docker hang).
		awsconfig.WithEC2IMDSClientEnableState(imds.ClientDisabled),
	)
	if err != nil {
		return nil, fmt.Errorf("sqs: load aws config: %w", err)
	}

	var opts []func(*sqs.Options)
	if cfg.AWSEndpointURL != "" {
		endpoint := cfg.AWSEndpointURL
		opts = append(opts, func(o *sqs.Options) {
			o.BaseEndpoint = aws.String(endpoint)
		})
	}

	return &Client{
		api:    sqs.NewFromConfig(awsCfg, opts...),
		wager:  cfg.SQSWagerQueueName,
		dlq:    cfg.SQSWagerDLQName,
		events: cfg.SQSEventsQueueName,
	}, nil
}

// CheckQueues verifies the wager, DLQ and events queues exist (GetQueueUrl).
func (c *Client) CheckQueues(ctx context.Context) error {
	wagerURL, err := c.queueURL(ctx, c.wager)
	if err != nil {
		return fmt.Errorf("sqs: wager queue %q: %w", c.wager, err)
	}
	dlqURL, err := c.queueURL(ctx, c.dlq)
	if err != nil {
		return fmt.Errorf("sqs: dlq %q: %w", c.dlq, err)
	}
	url, err := c.queueURL(ctx, c.events)
	if err != nil {
		return fmt.Errorf("sqs: events queue %q: %w", c.events, err)
	}
	c.mu.Lock()
	c.wagerURL = wagerURL
	c.dlqURL = dlqURL
	c.eventsURL = url
	c.mu.Unlock()
	return nil
}

func (c *Client) queueURL(ctx context.Context, name string) (string, error) {
	out, err := c.api.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{
		QueueName: aws.String(name),
	})
	if err != nil {
		return "", err
	}
	if out.QueueUrl == nil || *out.QueueUrl == "" {
		return "", fmt.Errorf("empty queue url")
	}
	return *out.QueueUrl, nil
}

func (c *Client) eventsQueueURL(ctx context.Context) (string, error) {
	c.mu.Lock()
	url := c.eventsURL
	c.mu.Unlock()
	if url != "" {
		return url, nil
	}
	got, err := c.queueURL(ctx, c.events)
	if err != nil {
		return "", fmt.Errorf("sqs: events queue %q: %w", c.events, err)
	}
	c.mu.Lock()
	c.eventsURL = got
	c.mu.Unlock()
	return got, nil
}

// Publish implements app.EventBus (ADR 0015).
func (c *Client) Publish(ctx context.Context, env app.Envelope) error {
	url, err := c.eventsQueueURL(ctx)
	if err != nil {
		return err
	}
	body, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("sqs: marshal envelope: %w", err)
	}
	_, err = c.api.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:               aws.String(url),
		MessageBody:            aws.String(string(body)),
		MessageGroupId:         aws.String(env.AggregateID),
		MessageDeduplicationId: aws.String(env.EventID),
	})
	if err != nil {
		return fmt.Errorf("sqs: send %s: %w", env.EventID, err)
	}
	return nil
}

// CreateFIFOQueue provisions a FIFO queue without content-based dedup (tests).
func (c *Client) CreateFIFOQueue(ctx context.Context, name string) error {
	_, err := c.api.CreateQueue(ctx, &sqs.CreateQueueInput{
		QueueName: aws.String(name),
		Attributes: map[string]string{
			"FifoQueue":                 "true",
			"ContentBasedDeduplication": "false",
		},
	})
	if err != nil {
		return fmt.Errorf("sqs: create queue %q: %w", name, err)
	}
	return nil
}

// DeleteQueue removes a queue (test cleanup).
func (c *Client) DeleteQueue(ctx context.Context, name string) error {
	url, err := c.queueURL(ctx, name)
	if err != nil {
		return err
	}
	_, err = c.api.DeleteQueue(ctx, &sqs.DeleteQueueInput{QueueUrl: aws.String(url)})
	if err != nil {
		return fmt.Errorf("sqs: delete queue %q: %w", name, err)
	}
	return nil
}

// ReceiveBodies long-polls the events queue. Used by integration tests.
func (c *Client) ReceiveBodies(ctx context.Context, max int32, waitSeconds int32) ([]string, error) {
	url, err := c.eventsQueueURL(ctx)
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
		return nil, fmt.Errorf("sqs: receive: %w", err)
	}
	bodies := make([]string, 0, len(out.Messages))
	for _, m := range out.Messages {
		if m.Body != nil {
			bodies = append(bodies, *m.Body)
		}
	}
	return bodies, nil
}

// Name identifies this dependency in readiness checks.
func (c *Client) Name() string {
	return "sqs"
}

// Check implements readiness by verifying configured queues exist.
func (c *Client) Check(ctx context.Context) error {
	return c.CheckQueues(ctx)
}

var _ app.EventBus = (*Client)(nil)
