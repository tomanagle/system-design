CREATE TABLE IF NOT EXISTS analytics.redirect_events
(
    version UInt8,
    event_id String,
    short_code String,
    occurred_at DateTime64(3, 'UTC'),
    user_id FixedString(64),
    kafka_topic LowCardinality(String),
    kafka_partition Int32,
    kafka_offset Int64
)
ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMM(occurred_at)
ORDER BY (short_code, occurred_at, event_id);
