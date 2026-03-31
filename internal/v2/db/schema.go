package db

func (dm *dbModule) initSourceSchema() error {
	createSourceSchema := `
	CREATE TABLE IF NOT EXISTS market_sources (
		id             VARCHAR(50)   PRIMARY KEY,
		source_name    VARCHAR(50)   NOT NULL,
		source_type    VARCHAR(16)   NOT NULL CHECK (source_type IN ('local', 'remote')),
		base_url       TEXT          NOT NULL DEFAULT '',
		priority       INTEGER       NOT NULL DEFAULT 100 CHECK (priority >= 0),
		is_active      BOOLEAN       NOT NULL DEFAULT true,
		description    TEXT          NOT NULL DEFAULT '',
		others         JSONB,
		created_at     TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
		updated_at     TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
		CONSTRAINT ck_market_sources_others_object
			CHECK (others IS NULL OR jsonb_typeof(others) = 'object')
	);`

	_, err := dm.db.Exec(createSourceSchema)
	if err != nil {
		return err
	}

	indexSourceSchema := `
	CREATE INDEX IF NOT EXISTS idx_market_sources_name ON market_sources (source_name);
	CREATE INDEX IF NOT EXISTS idx_market_sources_type ON market_sources (source_type);
	`

	_, err = dm.db.Exec(indexSourceSchema)
	if err != nil {
		return err
	}

	return nil
}

func (dm *dbModule) initAppSchema() error {
	createAppSchema := `
	CREATE TABLE IF NOT EXISTS market_apps (
		id                   BIGSERIAL     PRIMARY KEY,
		user_id              VARCHAR(64)   NOT NULL,
		source_id            VARCHAR(50)   NOT NULL REFERENCES market_sources(id) ON UPDATE CASCADE ON DELETE RESTRICT,
		app_id               VARCHAR(32)   NOT NULL,
		app_name             VARCHAR(100)  NOT NULL,
		app_version          VARCHAR(16)   NOT NULL,
		app_entry            JSONB,
		app_raw              JSONB,
		app_image_analysis   JSONB,
		is_removed           BOOLEAN       NOT NULL DEFAULT false,
		render_succeed       BOOLEAN       NOT NULL DEFAULT false,
		render_failed        BOOLEAN       NOT NULL DEFAULT false,
		render_failed_reason TEXT          NOT NULL DEFAULT '',
		created_at           TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
		updated_at           TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
		CONSTRAINT ck_market_apps_app_entry_json
			CHECK (app_entry IS NULL OR jsonb_typeof(app_entry) = 'object'),
		CONSTRAINT ck_market_apps_app_raw_json
			CHECK (app_raw IS NULL OR jsonb_typeof(app_raw) = 'object'),
		CONSTRAINT ck_market_apps_app_image_analysis_json
			CHECK (app_image_analysis IS NULL OR jsonb_typeof(app_image_analysis) = 'object')
	);`

	_, err := dm.db.Exec(createAppSchema)
	if err != nil {
		return err
	}

	indexAppSchema := `
	CREATE INDEX IF NOT EXISTS idx_market_apps_app_id ON market_apps (app_id);
	CREATE INDEX IF NOT EXISTS idx_market_apps_app_name ON market_apps (app_name);
	CREATE INDEX IF NOT EXISTS idx_market_apps_user_source_updated_at ON market_apps (user_id, source_id, updated_at);
	`

	_, err = dm.db.Exec(indexAppSchema)
	if err != nil {
		return err
	}

	return nil
}

func (dm *dbModule) initStateSchema() error {
	createStateSchema := `
	CREATE TABLE IF NOT EXISTS market_app_states (
		id                   BIGSERIAL      PRIMARY KEY,
		source_id            VARCHAR(50)    NOT NULL,
		app_id               VARCHAR(32)    NOT NULL,
		app_name             VARCHAR(128)   NOT NULL,
		app_raw_name         VARCHAR(100)   NOT NULL,
		app_version          VARCHAR(16)    NOT NULL,
		owner                VARCHAR(64)    NOT NULL,
		namespace            VARCHAR(100)   NOT NULL,
		is_sys_app           BOOLEAN        NOT NULL DEFAULT false,
		state                VARCHAR(64)    NOT NULL DEFAULT '',
		metadata             JSONB,
		spec                 JSONB,
		status               JSONB,
		created_at           TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
		updated_at           TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
		CONSTRAINT ck_market_app_states_metadata_object
			CHECK (metadata IS NULL OR jsonb_typeof(metadata) = 'object'),
		CONSTRAINT ck_market_app_states_spec_object
			CHECK (spec IS NULL OR jsonb_typeof(spec) = 'object'),
		CONSTRAINT ck_market_app_states_status_object
			CHECK (status IS NULL OR jsonb_typeof(status) = 'object')
	);`

	_, err := dm.db.Exec(createStateSchema)
	if err != nil {
		return err
	}

	indexStatesSchema := `
	CREATE INDEX IF NOT EXISTS idx_market_app_states_source_id ON market_app_states (source_id);
	CREATE INDEX IF NOT EXISTS idx_market_app_states_app_id ON market_app_states (app_id);
	CREATE INDEX IF NOT EXISTS idx_market_app_states_app_name ON market_app_states (app_name);
	CREATE INDEX IF NOT EXISTS idx_market_app_states_owner ON market_app_states (owner);
	CREATE INDEX IF NOT EXISTS idx_market_app_states_updated_at ON market_app_states (updated_at);
	`

	_, err = dm.db.Exec(indexStatesSchema)
	if err != nil {
		return err
	}

	return nil
}
