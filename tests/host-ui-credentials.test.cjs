const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const definitions = {};
let window;
let request;
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
                for (const child of item.items || []) { visit(child); }
            };
            visit(config);
            window = { fields, config, down: selector => fields[selector.slice(1)], show() { return this; } };
            return window;
        },
    },
    Proxmox: { Utils: {
        API2Request: options => { request = options; options.success({ result: { data: { token: 'one-time-token', 'server-fingerprint': 'aa:bb' } } }); },
        render_timestamp: value => value,
        format_size: value => value,
        format_boolean: value => value,
    } },
};
vm.createContext(context);
for (const file of ['Agent.js', 'Backups.js']) {
    vm.runInContext(fs.readFileSync(path.join(__dirname, '../www/host', file), 'utf8'), context);
}
definitions['PBS.HostAgentView'].controller.generateEnrollmentToken.call({ getView: () => view });
assert.equal(request.method, 'POST');
assert.equal(request.params, undefined, 'custom storage secrets must not be posted to the enrollment endpoint');
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
const restore = definitions['PBS.window.HostRestore'];
let restoreConfig;
const restoreWindow = { host: 'source-host', store: 'backup', backupTime: 123, callParent() { restoreConfig = this.items; } };
restore.initComponent.call(restoreWindow);
const selector = restoreConfig.column2.find(item => item.name === 'target-host');
assert.equal(selector.value, 'source-host');
assert.equal(selector.forceSelection, true);
assert.equal(restoreWindow.url, '/api2/extjs/admin/host/source-host/restore');
console.log('Host credential enrollment and restore-destination UI checks passed');
