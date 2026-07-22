CREATE TABLE IF NOT EXISTS click_stats_device (
    link_id BIGINT      NOT NULL REFERENCES links(id) ON DELETE CASCADE,
    day     DATE        NOT NULL,
    device  VARCHAR(20) NOT NULL,
    browser VARCHAR(40) NOT NULL,
    os      VARCHAR(40) NOT NULL,
    clicks  BIGINT      NOT NULL DEFAULT 0,
    PRIMARY KEY (link_id, day, device, browser, os)
);
