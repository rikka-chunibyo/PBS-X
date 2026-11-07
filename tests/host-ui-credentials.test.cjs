const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const definitions = {};
let window;
let request;
let failNext = false;
let deferNext = false;
let pending;
let serial = 0;
const view = { host: 'destination-device' };
const context = {
    gettext: value => value,
    TextEncoder,
    btoa: value => Buffer.from(value, 'binary').toString('base64'),
    window: { location: { origin: 'https://pbs.example:8007' } },
    Ext: {
        define: (name, config) => { definitions[name] = config; },
        String: { htmlEncode: value => value },
        create: (name, config) => {
            const fields = {};
            const visit = item => {
                if (item.itemId) {
                    fields[item.itemId] = {
                        value: item.value || '', disabled: !!item.disabled, config: item,
                        getValue() { return this.value; },
                        setValue(value) { this.value = value; },
                        setDisabled(value) { this.disabled = value; },
                        up() { return window; },
                    };
                }
                for (const child of [...item.items || [], ...item.buttons || []]) { visit(child); }
            };
            visit(config);
            window = { fields, config, down: selector => fields[selector.slice(1)], show() { return this; } };
            return window;
        },
    },
    Proxmox: { Utils: {
        API2Request: options => {
            request = options;
            const complete = () => {
                if (failNext) { failNext = false; options.failure({htmlStatus: 'fixture failure'}); }
                else { options.success({ result: { data: { token: `one-time-token-${++serial}`, 'server-fingerprint': 'aa:bb' } } }); }
                options.callback?.();
            };
            if (deferNext) { deferNext = false; pending = complete; } else { complete(); }
        },
        render_timestamp: value => value,
        format_size: value => value,
        format_boolean: value => value,
    } },
};
context.Ext.Msg = { alert() {} };
vm.createContext(context);
for (const file of ['Agent.js', 'Backups.js']) {
    vm.runInContext(fs.readFileSync(path.join(__dirname, '../www/host', file), 'utf8'), context);
}
definitions['PBS.HostAgentView'].controller.generateEnrollmentToken.call({ getView: () => view });
assert.equal(request.method, 'POST');
assert.equal(JSON.stringify(request.params), '{"revoke-existing-keys":false}');
assert.equal(window.fields.revokeExistingKeys.config.checked, false);
const decode = () => JSON.parse(Buffer.from(window.fields.enrollmentCode.value.slice('pbsx1.'.length), 'base64url'));
assert.equal(decode().storage_auth_id, undefined);
assert.equal(window.fields.storageAuthID.disabled, true);
window.fields.storageMode.value = 'custom';
window.fields.storageAuthID.value = 'reader@pbs!shared';
window.fields.storageSecret.value = 'test-secret';
window.fields.storageMode.config.listeners.change(window.fields.storageMode, 'custom');
assert.equal(decode().storage_auth_id, 'reader@pbs!shared');
assert.equal(decode().storage_secret, 'test-secret');
assert.equal(window.fields.storageSecret.config.inputType, 'password');
assert.equal(window.fields.storageSecret.disabled, false);
window.fields.storageMode.value = 'auto';
window.fields.storageMode.config.listeners.change(window.fields.storageMode, 'auto');
assert.equal(decode().storage_secret, undefined, 'automatic codes must not retain a custom secret');
assert.equal(decode().host_id, 'destination-device');
let first = decode().bootstrap_token;
window.fields.revokeExistingKeys.value = true;
window.fields.revokeExistingKeys.config.listeners.change(window.fields.revokeExistingKeys);
assert.equal(request.params['revoke-existing-keys'], true);
assert.notEqual(decode().bootstrap_token, first);
assert.equal(decode()['revoke-existing-keys'], undefined, 'the server grant owns the choice, not the device payload');
window.fields.revokeExistingKeys.value = false;
deferNext = true;
window.fields.revokeExistingKeys.config.listeners.change(window.fields.revokeExistingKeys);
assert.equal(window.fields.enrollmentCode.value, '');
assert.equal(window.fields.copyEnrollment.disabled, true);
assert.equal(window.fields.revokeExistingKeys.disabled, true);
window.fields.storageMode.config.listeners.change(window.fields.storageMode, 'auto');
assert.equal(window.fields.enrollmentCode.value, '', 'form edits cannot expose an older grant while regenerating');
pending();
assert.equal(request.params['revoke-existing-keys'], false);
assert.equal(window.fields.copyEnrollment.disabled, false);
assert.equal(window.fields.revokeExistingKeys.disabled, false);
failNext = true;
window.fields.regenerateEnrollment.config.handler({up: () => window});
assert.equal(window.fields.enrollmentCode.value, '');
assert.equal(window.fields.copyEnrollment.disabled, true, 'failed regeneration must not leave a stale code copyable');
window.fields.regenerateEnrollment.config.handler({up: () => window});
assert.ok(decode().bootstrap_token);
assert.equal(Object.keys(request.params).length, 1, 'custom storage secrets must never be posted to the endpoint');
const restore = definitions['PBS.window.HostRestore'];
let restoreConfig;
const restoreWindow = { host: 'source-host', store: 'backup', backupTime: 123, callParent() { restoreConfig = this.items; } };
restore.initComponent.call(restoreWindow);
const selector = restoreConfig.column2.find(item => item.name === 'target-host');
assert.equal(selector.value, 'source-host');
assert.equal(selector.forceSelection, true);
assert.equal(restoreWindow.url, '/api2/extjs/admin/host/source-host/restore');
console.log('Host credential enrollment and restore-destination UI checks passed');
