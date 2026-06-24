-- +goose Up
-- +goose StatementBegin
create table if not exists auth_users (
  connection_id text primary key,
  native_auth_id text not null unique,
  native_meta json not null
);

create table if not exists sync_state (
  key text primary key,
  value text not null
);

create table if not exists connection_traffic_state (
  connection_id text primary key,
  observed_rx_bytes bigint not null default 0 check(observed_rx_bytes >= 0),
  observed_tx_bytes bigint not null default 0 check(observed_tx_bytes >= 0),
  acknowledged_rx_bytes bigint not null default 0 check(acknowledged_rx_bytes >= 0),
  acknowledged_tx_bytes bigint not null default 0 check(acknowledged_tx_bytes >= 0),
  unacknowledged_rx_bytes bigint not null default 0 check(unacknowledged_rx_bytes >= 0),
  unacknowledged_tx_bytes bigint not null default 0 check(unacknowledged_tx_bytes >= 0),
  sent_rx_bytes bigint not null default 0 check(sent_rx_bytes >= 0),
  sent_tx_bytes bigint not null default 0 check(sent_tx_bytes >= 0),
  request_id text,
  check((request_id is null and sent_rx_bytes = 0 and sent_tx_bytes = 0) or
        (request_id is not null and sent_rx_bytes <= unacknowledged_rx_bytes and sent_tx_bytes <= unacknowledged_tx_bytes))
);

create index if not exists idx_connection_traffic_request
on connection_traffic_state(request_id, connection_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
drop table if exists connection_traffic_state;
drop table if exists sync_state;
drop table if exists auth_users;
-- +goose StatementEnd
