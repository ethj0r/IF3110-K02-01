-- +goose Up
CREATE TABLE group_leaders (
    group_id UUID PRIMARY KEY,
    user_id UUID NOT NULL,
    FOREIGN KEY (user_id, group_id) REFERENCES user_groups (user_id, group_id) ON DELETE CASCADE
);

-- +goose Down
DROP TABLE group_leaders;
