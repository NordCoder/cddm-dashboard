import test from "node:test";
import assert from "node:assert/strict";
import { memoryStorage } from "../src/ledger.js";
import { replaceBackendOrigin } from "../src/options.js";
import { backendPermissionOrigin } from "../src/protocol.js";

function permissionFixture(initial = []) {
  const granted = new Set(initial);
  const calls = { request: [], remove: [] };
  return {
    granted,
    calls,
    api: {
      async contains({ origins }) { return origins.every((origin) => granted.has(origin)); },
      async request({ origins }) { calls.request.push(...origins); origins.forEach((origin) => granted.add(origin)); return true; },
      async remove({ origins }) { calls.remove.push(...origins); const had = origins.every((origin) => granted.has(origin)); origins.forEach((origin) => granted.delete(origin)); return had; }
    }
  };
}

test("switching backend origin revokes the previous permission and keeps only the new origin", async () => {
  const oldOrigin = "http://localhost:8080";
  const nextOrigin = "http://127.0.0.1:8081";
  const storage = memoryStorage({ backend_origin: oldOrigin });
  const permissions = permissionFixture([backendPermissionOrigin(oldOrigin)]);

  assert.equal(await replaceBackendOrigin({ storage, permissions: permissions.api }, nextOrigin), nextOrigin);
  assert.equal(storage.values.backend_origin, nextOrigin);
  assert.equal(permissions.granted.has(backendPermissionOrigin(oldOrigin)), false);
  assert.equal(permissions.granted.has(backendPermissionOrigin(nextOrigin)), true);
  assert.deepEqual(permissions.calls.remove, [backendPermissionOrigin(oldOrigin)]);
});

test("permission denial leaves the existing backend untouched", async () => {
  const oldOrigin = "http://localhost:8080";
  const nextOrigin = "https://backend.example";
  const storage = memoryStorage({ backend_origin: oldOrigin });
  const oldPermission = backendPermissionOrigin(oldOrigin);
  const permissions = permissionFixture([oldPermission]);
  permissions.api.request = async () => false;

  await assert.rejects(() => replaceBackendOrigin({ storage, permissions: permissions.api }, nextOrigin), /backend_permission_denied/);
  assert.equal(storage.values.backend_origin, oldOrigin);
  assert.deepEqual([...permissions.granted], [oldPermission]);
});

test("failure to revoke the previous origin rolls configuration back and removes the new grant", async () => {
  const oldOrigin = "http://localhost:8080";
  const nextOrigin = "https://backend.example";
  const storage = memoryStorage({ backend_origin: oldOrigin });
  const oldPermission = backendPermissionOrigin(oldOrigin);
  const nextPermission = backendPermissionOrigin(nextOrigin);
  const permissions = permissionFixture([oldPermission]);
  permissions.api.remove = async ({ origins }) => {
    permissions.calls.remove.push(...origins);
    if (origins.includes(oldPermission)) return false;
    origins.forEach((origin) => permissions.granted.delete(origin));
    return true;
  };

  await assert.rejects(() => replaceBackendOrigin({ storage, permissions: permissions.api }, nextOrigin), /previous_backend_permission_not_revoked/);
  assert.equal(storage.values.backend_origin, oldOrigin);
  assert.equal(permissions.granted.has(oldPermission), true);
  assert.equal(permissions.granted.has(nextPermission), false);
});
