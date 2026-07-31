package database

import (
	"context"
	"database/sql"
	"testing"
)

func TestProvisioningWorkerSessionMigrationBackfillsOrQuarantinesVersion17Records(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite3", sqliteDSN(":memory:"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })

	_, err = db.ExecContext(ctx, `
		CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY, name TEXT NOT NULL);
		CREATE TABLE session_provision_requests(
			id TEXT PRIMARY KEY,
			project_id INTEGER NOT NULL,
			worker_id TEXT NOT NULL DEFAULT '',
			bound_binding_id TEXT NOT NULL DEFAULT '',
			bound_binding_version INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL,
			completion_reason TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE autonomous_command_materializations(
			id TEXT PRIMARY KEY,
			project_id INTEGER NOT NULL,
			provision_request_id TEXT NOT NULL,
			delivery_command_id TEXT NOT NULL,
			created_at TEXT NOT NULL
		);
		CREATE TABLE delivery_commands(
			id TEXT PRIMARY KEY,
			project_id INTEGER NOT NULL,
			worker_id TEXT NOT NULL,
			worker_session_id TEXT NOT NULL DEFAULT '',
			binding_id TEXT NOT NULL,
			binding_version INTEGER NOT NULL
		);
		INSERT INTO session_provision_requests(
			id,project_id,worker_id,bound_binding_id,bound_binding_version,status,completion_reason
		) VALUES
			('provision-linked',1,'worker-one','binding-one',3,'provisioned','exact_session_bound'),
			('provision-conflicting',1,'worker-two','binding-two',4,'provisioned','exact_session_bound'),
			('provision-standalone',1,'worker-three','binding-three',1,'provisioned','exact_session_bound'),
			('provision-pending',1,'','','0','pending','');
		INSERT INTO delivery_commands(
			id,project_id,worker_id,worker_session_id,binding_id,binding_version
		) VALUES
			('delivery-linked',1,'worker-one','session-from-v17','binding-one',3),
			('delivery-conflicting',1,'wrong-worker','session-wrong','binding-two',4);
		INSERT INTO autonomous_command_materializations(
			id,project_id,provision_request_id,delivery_command_id,created_at
		) VALUES
			('materialization-linked',1,'provision-linked','delivery-linked','2026-07-30T00:00:00Z'),
			('materialization-conflicting',1,'provision-conflicting','delivery-conflicting','2026-07-30T00:00:01Z');
		PRAGMA user_version = 17;
	`)
	if err != nil {
		t.Fatalf("create version-17 fixture: %v", err)
	}

	migration, err := migrationFiles.ReadFile("migrations/0018_provisioning_worker_session.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := runMigration(ctx, db, 18, "0018_provisioning_worker_session.sql", string(migration)); err != nil {
		t.Fatal(err)
	}

	assertProvisionMigrationRow(t, db, "provision-linked", "provisioned", "session-from-v17", "exact_session_bound")
	assertProvisionMigrationRow(t, db, "provision-conflicting", "uncertain", "", "exact_session_bound;missing_durable_session_identity_after_upgrade")
	assertProvisionMigrationRow(t, db, "provision-standalone", "uncertain", "", "exact_session_bound;missing_durable_session_identity_after_upgrade")
	assertProvisionMigrationRow(t, db, "provision-pending", "pending", "", "")

	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 18 {
		t.Fatalf("schema version = %d, want 18", version)
	}
}

func assertProvisionMigrationRow(t *testing.T, db *sql.DB, id, wantStatus, wantSession, wantReason string) {
	t.Helper()
	var status, session, reason string
	if err := db.QueryRow(`SELECT status,worker_session_id,completion_reason FROM session_provision_requests WHERE id=?`, id).Scan(&status, &session, &reason); err != nil {
		t.Fatal(err)
	}
	if status != wantStatus || session != wantSession || reason != wantReason {
		t.Fatalf("%s = status %q session %q reason %q, want %q %q %q", id, status, session, reason, wantStatus, wantSession, wantReason)
	}
}
