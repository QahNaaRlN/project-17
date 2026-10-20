-- +goose Up
-- STU-355…374: requests for missing application components. The placeholder
-- node id is allocated at create; placement and initial props are inserted at
-- submit. Component anchors are nodes (empty event).
ALTER TABLE capability_requests DROP CONSTRAINT capability_requests_kind_check;
ALTER TABLE capability_requests
  ADD CONSTRAINT capability_requests_kind_check CHECK (kind IN ('action', 'component')),
  ADD COLUMN placement jsonb,
  ADD COLUMN initial_props jsonb,
  ADD CONSTRAINT capability_requests_component_anchor CHECK ((kind = 'component') = (event = '' AND placement IS NOT NULL));

ALTER TABLE capability_request_anchors
  ADD COLUMN anchor_kind text NOT NULL DEFAULT 'event' CHECK (anchor_kind IN ('event', 'node')),
  ADD CONSTRAINT capability_anchor_node_event CHECK ((anchor_kind = 'node') = (event = ''));
ALTER TABLE capability_request_anchors DROP CONSTRAINT capability_request_anchors_state_check;
ALTER TABLE capability_request_anchors ADD CONSTRAINT capability_request_anchors_state_check
  CHECK (state IN ('active', 'queued', 'replaced', 'awaiting_args', 'invalid_args', 'invalid_props', 'anchor_changed', 'changeset_in_review', 'changeset_closed', 'reverted', 'detached', 'failed'));

-- +goose Down
-- Component requests cannot be represented by the action-only schema; they
-- must be cancelled and removed by an operator first (no silent data loss).
-- +goose StatementBegin
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM capability_requests WHERE kind = 'component') THEN
    RAISE EXCEPTION 'component capability requests exist';
  END IF;
END
$$;
-- +goose StatementEnd
UPDATE capability_request_anchors SET state = 'invalid_args' WHERE state = 'invalid_props';
ALTER TABLE capability_request_anchors DROP CONSTRAINT capability_request_anchors_state_check;
ALTER TABLE capability_request_anchors ADD CONSTRAINT capability_request_anchors_state_check
  CHECK (state IN ('active', 'queued', 'replaced', 'awaiting_args', 'invalid_args', 'anchor_changed', 'changeset_in_review', 'changeset_closed', 'reverted', 'detached', 'failed'));
ALTER TABLE capability_request_anchors DROP CONSTRAINT capability_anchor_node_event, DROP COLUMN anchor_kind;
ALTER TABLE capability_requests
  DROP CONSTRAINT capability_requests_component_anchor,
  DROP COLUMN initial_props,
  DROP COLUMN placement,
  DROP CONSTRAINT capability_requests_kind_check;
ALTER TABLE capability_requests ADD CONSTRAINT capability_requests_kind_check CHECK (kind = 'action');
