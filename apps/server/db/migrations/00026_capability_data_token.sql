-- +goose Up
-- STU-375…394: requests for missing data sources and tokens. A data source
-- anchor stores the page's data source key in node_id; a token anchor stores
-- the node and the design path (event), e.g. "padding/lg".
ALTER TABLE capability_requests DROP CONSTRAINT capability_requests_kind_check;
ALTER TABLE capability_requests DROP CONSTRAINT capability_requests_component_anchor;
ALTER TABLE capability_requests
  ADD CONSTRAINT capability_requests_kind_check CHECK (kind IN ('action', 'component', 'dataSource', 'token')),
  ADD CONSTRAINT capability_requests_anchor_shape CHECK (
    (kind = 'action' AND event <> '' AND placement IS NULL)
    OR (kind IN ('component', 'dataSource') AND event = '' AND placement IS NOT NULL)
    OR (kind = 'token' AND event <> '' AND placement IS NOT NULL));

ALTER TABLE capability_request_anchors DROP CONSTRAINT capability_anchor_node_event;
ALTER TABLE capability_request_anchors DROP CONSTRAINT capability_request_anchors_anchor_kind_check;
ALTER TABLE capability_request_anchors
  ADD CONSTRAINT capability_request_anchors_anchor_kind_check CHECK (anchor_kind IN ('event', 'node', 'source', 'design')),
  ADD CONSTRAINT capability_anchor_node_event CHECK ((anchor_kind IN ('node', 'source')) = (event = ''));

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM capability_requests WHERE kind IN ('dataSource', 'token')) THEN
    RAISE EXCEPTION 'data source or token capability requests exist';
  END IF;
END
$$;
-- +goose StatementEnd
ALTER TABLE capability_request_anchors DROP CONSTRAINT capability_anchor_node_event;
ALTER TABLE capability_request_anchors DROP CONSTRAINT capability_request_anchors_anchor_kind_check;
ALTER TABLE capability_request_anchors
  ADD CONSTRAINT capability_request_anchors_anchor_kind_check CHECK (anchor_kind IN ('event', 'node')),
  ADD CONSTRAINT capability_anchor_node_event CHECK ((anchor_kind = 'node') = (event = ''));
ALTER TABLE capability_requests DROP CONSTRAINT capability_requests_anchor_shape;
ALTER TABLE capability_requests DROP CONSTRAINT capability_requests_kind_check;
ALTER TABLE capability_requests
  ADD CONSTRAINT capability_requests_kind_check CHECK (kind IN ('action', 'component')),
  ADD CONSTRAINT capability_requests_component_anchor CHECK ((kind = 'component') = (event = '' AND placement IS NOT NULL));
