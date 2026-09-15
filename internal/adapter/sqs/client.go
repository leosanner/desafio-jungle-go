package sqsadapter

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"github.com/leosanner/desafio-jungle-go/internal/config"
)

// Client is an SQS adapter used for readiness and (later) consume/publish.
type Client struct {
	api   queueAPI
	wager string
	dlq   string
}

type queueAPI interface {
	GetQueueUrl(ctx context.Context, params *sqs.GetQueueUrlInput, optFns ...func(*sqs.Options)) (*sqs.GetQueueUrlOutput, error)
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
		api:   sqs.NewFromConfig(awsCfg, opts...),
		wager: cfg.SQSWagerQueueName,
		dlq:   cfg.SQSWagerDLQName,
	}, nil
}

// CheckQueues verifies both the wager queue and the DLQ exist (GetQueueUrl).
func (c *Client) CheckQueues(ctx context.Context) error {
	if _, err := c.api.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{
		QueueName: aws.String(c.wager),
	}); err != nil {
		return fmt.Errorf("sqs: wager queue %q: %w", c.wager, err)
	}
	if _, err := c.api.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{
		QueueName: aws.String(c.dlq),
	}); err != nil {
		return fmt.Errorf("sqs: dlq %q: %w", c.dlq, err)
	}
	return nil
}

// Name identifies this dependency in readiness checks.
func (c *Client) Name() string {
	return "sqs"
}

// Check implements readiness by verifying both queues exist.
func (c *Client) Check(ctx context.Context) error {
	return c.CheckQueues(ctx)
}
