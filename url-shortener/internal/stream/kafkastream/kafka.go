package kafkastream

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/segmentio/kafka-go"
)

func NewKafkaWriter(brokers []string, topic string) *kafka.Writer {
	return &kafka.Writer{
		Addr: kafka.TCP(brokers...), Topic: topic,
		Balancer: &kafka.Hash{}, RequiredAcks: kafka.RequireAll,
		BatchSize: 100, BatchTimeout: 10 * time.Millisecond,
		ReadTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second,
		MaxAttempts: 3,
		// Topic creation and partition validation belong to the worker startup.
		AllowAutoTopicCreation: false,
	}
}

func NewKafkaReader(brokers []string, topic, group string) *kafka.Reader {
	return kafka.NewReader(kafka.ReaderConfig{
		Brokers: brokers, Topic: topic, GroupID: group,
		MinBytes: 1, MaxBytes: 10e6, MaxWait: time.Second,
		StartOffset: kafka.FirstOffset, CommitInterval: 0,
	})
}

func MustEnsureTopic(ctx context.Context, brokers []string, topic string, partitions int) {
	client := &kafka.Client{Addr: kafka.TCP(brokers...), Timeout: 10 * time.Second}
	response, err := client.CreateTopics(ctx, &kafka.CreateTopicsRequest{Topics: []kafka.TopicConfig{{
		Topic: topic, NumPartitions: partitions, ReplicationFactor: 1,
		ConfigEntries: []kafka.ConfigEntry{{ConfigName: "retention.ms", ConfigValue: "604800000"}},
	}}})
	if err != nil {
		panic(fmt.Errorf("create analytics topic: %w", err))
	}
	if err := response.Errors[topic]; err != nil && !errors.Is(err, kafka.TopicAlreadyExists) {
		panic(err)
	}
	metadata, err := client.Metadata(ctx, &kafka.MetadataRequest{Topics: []string{topic}})
	if err != nil {
		panic(fmt.Errorf("read analytics topic: %w", err))
	}
	if len(metadata.Topics) != 1 || metadata.Topics[0].Error != nil {
		panic("analytics topic metadata unavailable")
	}
	if got := len(metadata.Topics[0].Partitions); got != partitions {
		panic(fmt.Sprintf("analytics topic has %d partitions, configured %d; do not repartition an active ordered stream", got, partitions))
	}
}
