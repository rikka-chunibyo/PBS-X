const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const definitions = {};
let request, created, alert;
const context = {
    gettext: value => value, TextDecoder, Uint8Array,
    atob: value => atob(value),
    Ext: {
        define: (name, value) => { definitions[name] = value; },
        String: { htmlEncode: value => value },
        create: (name, config) => { created = { name, config }; return { show() {} }; },
        Msg: { alert: (...args) => { alert = args; } },
    },
    Proxmox: { Utils: {
        API2Request: value => { request = value; },
        render_timestamp() {}, format_size() {}, format_boolean() {},
    } },
};
vm.createContext(context);
vm.runInContext(fs.readFileSync(require('node:path').join(__dirname, '../www/host/Backups.js'), 'utf8'), context);
const browser = definitions['PBS.HostBackupBrowser'];
context.PBS = { HostBackupBrowser: browser };
const encoded = path => Buffer.from(path).toString('base64');
assert.equal(browser.restorePath(encoded('/root.pxar.didx/Documents/写真.txt'), 'root.pxar.didx'), '/Documents/写真.txt');
assert.equal(browser.restorePath(encoded('/root.pxar.didx'), 'root.pxar.didx'), '/');
assert.equal(browser.restorePath(encoded('/Documents/写真.txt'), 'root.mpxar.didx'), '/Documents/写真.txt');
assert.equal(browser.restorePath(encoded('/'), 'root.mpxar.didx'), '/');
for (const [path, archive] of [
    ['/root.pxar.didx/../escape', 'root.pxar.didx'],
    ['/another.pxar.didx/private', 'root.pxar.didx'],
    ['/root.pxar.didx-other/private', 'root.pxar.didx'],
    ['/root.pxar.didx/file\nother', 'root.pxar.didx'],
    ['/root.pxar.didx/file\0', 'root.pxar.didx'],
    ['/root.pxar.didx/file ', 'root.pxar.didx'],
    ['/root.pxar.didx/a\\b', 'root.pxar.didx'],
    ['relative/path', 'root.mpxar.didx'],
    ['/file', 'other.mpxar.didx'],
]) assert.throws(() => browser.restorePath(encoded(path), archive), `${path} must not select a different restore target`);
assert.throws(() => browser.restorePath('@@@', 'root.pxar.didx'));
assert.throws(() => browser.restorePath(Buffer.from([47, 255]).toString('base64'), 'root.mpxar.didx'));
const snapshot = { store: 'backup store', ns: 'family/phone', 'backup-id': 'photos-id', 'backup-time': 1780000000 };
const view = { host: 'original-host', getSelection: () => [{ data: snapshot }] };
definitions['PBS.HostBackupSnapshotView'].controller.browseSnapshot.call({ getView: () => view });
assert.equal(request.url, '/api2/json/admin/datastore/backup%20store/files');
assert.equal(request.params.ns, 'family/phone');
assert.equal(request.params['backup-id'], 'photos-id');
request.success({ result: { data: [{ filename: 'root.mpxar.didx', 'crypt-mode': 'none' }] } });
assert.equal(created.name, 'PBS.window.HostBackupBrowser');
let fields, restoreButton, selectedHandler;
const instance = {
    ...created.config,
    callParent() {},
    down: () => ({
        getSelection: () => [{ data: { filepath: encoded('/Documents/写真.txt') } }],
        on: (event, handler) => { selectedHandler = handler; },
    }),
    addDocked: config => {
        restoreButton = config.items[0];
        return [{ items: { getAt: () => ({ setDisabled: value => { restoreButton.disabled = value; } }) } }];
    },
};
definitions['PBS.window.HostBackupBrowser'].initComponent.call(instance);
assert.equal(instance.extraParams['archive-name'], 'root.mpxar.didx');
assert.equal(instance.extraParams.ns, 'family/phone');
assert.equal(instance.extraParams['backup-id'], 'photos-id');
selectedHandler(null, [1]);
assert.equal(restoreButton.disabled, false);
restoreButton.handler();
assert.equal(created.name, 'PBS.window.HostRestore');
assert.equal(created.config.backupID, 'photos-id');
assert.equal(created.config.ns, 'family/phone');
assert.equal(created.config.sources[0], '/Documents/写真.txt');
const form = { ...created.config, callParent() {} };
definitions['PBS.window.HostRestore'].initComponent.call(form);
assert.equal(form.items.columnB[0].value, '/Documents/写真.txt');
assert.equal(form.items.column2[0].name, 'target-host');
created = undefined;
request.success({ result: { data: [{ filename: 'root.mpxar.didx', 'crypt-mode': 'encrypt' }] } });
assert.equal(created, undefined);
assert.match(alert[1], /unencrypted/);
console.log('Host browser path encoding, snapshot identity, selected restore and encrypted-archive checks passed');
