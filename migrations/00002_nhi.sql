-- +goose Up
-- NHI envanteri + iliski grafigi semasi: docs/DECISIONS.md Karar 1 ve
-- Karar 2'de onaylandi (issue #1, GZ-S0). GZ-A1/GZ-A2/GZ-B1'in guvenlik
-- tasarimina ait; kolon yapisi burada donuk, icerik (sahte NHI'nin
-- owner/scope alanlarinin nasil "inandirici" olacagi, blast-radius
-- implementasyonunun Postgres recursive CTE mi yoksa uygulama katmaninda
-- mi yapilacagi) GZ-A2/GZ-B1'in alani.

CREATE TABLE nhi_identities (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    type            text NOT NULL,             -- service_account / token / machine_identity
    owner           text NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    last_used_at    timestamptz,
    scope           text NOT NULL DEFAULT '',
    secret_ref_hash text,                      -- HMAC ozeti; sir asla ham tutulmaz
    is_decoy        boolean NOT NULL DEFAULT false
);

CREATE INDEX idx_nhi_identities_type ON nhi_identities (type);
CREATE INDEX idx_nhi_identities_is_decoy ON nhi_identities (is_decoy);

-- Tek kenar tipi, yonlu: "from_id, to_id'ye erisebilir" (docs/DECISIONS.md
-- Karar 2). relation kolonu simdiden var (v0.1'de tek deger alsa da) —
-- v0.2'de sahiplik/devir eklenince yeni bir migration/tablo gerekmez.
CREATE TABLE nhi_edges (
    from_id  uuid NOT NULL REFERENCES nhi_identities (id) ON DELETE CASCADE,
    to_id    uuid NOT NULL REFERENCES nhi_identities (id) ON DELETE CASCADE,
    relation text NOT NULL DEFAULT 'access',
    PRIMARY KEY (from_id, to_id)
);

CREATE INDEX idx_nhi_edges_to_id ON nhi_edges (to_id);

-- +goose Down
DROP TABLE IF EXISTS nhi_edges;
DROP TABLE IF EXISTS nhi_identities;
