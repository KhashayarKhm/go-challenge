-- One row per (segment, day, user): the fact "user was tagged with segment on day".
--
-- * ORDER BY (segment, day, user_id): estimate filters on segment + a day range,
--   so the primary key prunes the scan to one segment's last 14 days. Putting the
--   low-cardinality segment first also gives long runs that compress very well.
-- * ReplacingMergeTree: redeliveries / repeated taggings on the same day produce
--   identical keys and are collapsed during background merges. Merges are eventual,
--   so queries must count distinct users (uniq) and never rely on count().
-- * Rows of the same user on different days are intentional history, not duplicates.
-- * PARTITION BY month and no TTL: history is kept on purpose for future analytics;
--   monthly partitions keep the partition count low as it grows. The 2-week rule is
--   enforced by the estimate query only.
CREATE TABLE IF NOT EXISTS segment_users
(
    segment LowCardinality(String),
    day     Date,
    user_id String
)
ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMM(day)
ORDER BY (segment, day, user_id);
