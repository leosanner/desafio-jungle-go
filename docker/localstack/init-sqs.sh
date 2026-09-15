#!/usr/bin/env bash
# Provision the challenge FIFO queues and redrive (init.md §10). Idempotent.
set -euo pipefail

REGION="${AWS_DEFAULT_REGION:-us-east-1}"
ACCOUNT="${AWS_ACCOUNT_ID:-000000000000}"
DLQ_NAME="wager-transactions-dlq.fifo"
MAIN_NAME="wager-transactions.fifo"

awslocal sqs create-queue \
	--queue-name "${DLQ_NAME}" \
	--attributes FifoQueue=true,ContentBasedDeduplication=true >/dev/null

DLQ_URL="$(awslocal sqs get-queue-url --queue-name "${DLQ_NAME}" --query QueueUrl --output text)"
DLQ_ARN="$(awslocal sqs get-queue-attributes \
	--queue-url "${DLQ_URL}" \
	--attribute-names QueueArn \
	--query Attributes.QueueArn \
	--output text)"

if [ -z "${DLQ_ARN}" ] || [ "${DLQ_ARN}" = "None" ]; then
	DLQ_ARN="arn:aws:sqs:${REGION}:${ACCOUNT}:${DLQ_NAME}"
fi

REDRIVE="$(printf '{"deadLetterTargetArn":"%s","maxReceiveCount":"5"}' "${DLQ_ARN}")"

awslocal sqs create-queue \
	--queue-name "${MAIN_NAME}" \
	--attributes FifoQueue=true,ContentBasedDeduplication=true >/dev/null

MAIN_URL="$(awslocal sqs get-queue-url --queue-name "${MAIN_NAME}" --query QueueUrl --output text)"
awslocal sqs set-queue-attributes \
	--queue-url "${MAIN_URL}" \
	--attributes "RedrivePolicy=${REDRIVE}"
