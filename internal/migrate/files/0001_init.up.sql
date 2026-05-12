CREATE TABLE bookwarehouse_audio.forwarded_request (
  request_id   text PRIMARY KEY,
  external_id  text,
  status       text NOT NULL,
  last_polled  timestamptz,
  error_text   text,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX forwarded_request_status_polled_idx
  ON bookwarehouse_audio.forwarded_request (status, last_polled);
