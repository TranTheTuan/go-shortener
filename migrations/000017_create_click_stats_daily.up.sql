CREATE TABLE IF NOT EXISTS click_stats_daily (
    link_id BIGINT NOT NULL REFERENCES links(id) ON DELETE CASCADE,
    day     DATE   NOT NULL,
    clicks  BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (link_id, day)
);
