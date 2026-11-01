Ext.define('PBS.HostAgentView', {
    extend: 'Ext.grid.Panel',
    alias: 'widget.pbsHostAgentView',

    title: gettext('Agent'),
    iconCls: 'fa fa-exchange',

    controller: {
	xclass: 'Ext.app.ViewController',

	generateEnrollmentToken: function() {
	    let view = this.getView();
	    if (view.enrollmentWindow) { view.enrollmentWindow.show(); return; }
	    Proxmox.Utils.API2Request({
		method: 'POST',
		url: `/config/host/${encodeURIComponent(view.host)}/enrollment-token`,
		params: { 'revoke-existing-keys': false },
		success: function(response) {
		    let data = response.result.data;
		    let origin = window.location.origin;
		    let encodeEnrollment = function(server) {
			if (!data) { return ''; }
			let fields = {
			    v: 1,
			    host_id: view.host,
			    server_url: server,
			    bootstrap_token: data.token,
			    server_ca_fingerprint: data['server-fingerprint'],
			};
			let form = view.enrollmentWindow;
			if (form && form.down('#storageMode').getValue() === 'custom') {
			    fields.storage_auth_id = form.down('#storageAuthID').getValue().trim();
			    fields.storage_secret = form.down('#storageSecret').getValue();
			}
			let payload = JSON.stringify(fields);
			let binary = '';
			for (let byte of new TextEncoder().encode(payload)) {
			    binary += String.fromCharCode(byte);
			}
			return `pbsx1.${btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')}`;
		    };
		    let initialCode = encodeEnrollment(origin);
		    let refreshEnrollment = function(win) {
			data = null;
			win.down('#enrollmentCode').setValue('');
			win.down('#tokenField').setValue('');
			win.down('#copyEnrollment').setDisabled(true);
			win.down('#regenerateEnrollment').setDisabled(true);
			win.down('#revokeExistingKeys').setDisabled(true);
			Proxmox.Utils.API2Request({
			    method: 'POST',
			    url: `/config/host/${encodeURIComponent(view.host)}/enrollment-token`,
			    params: { 'revoke-existing-keys': win.down('#revokeExistingKeys').getValue() === true },
			    success: function(result) {
				if (win.destroyed) { return; }
				data = result.result.data;
				win.down('#tokenField').setValue(data.token);
				win.down('#fingerprintField').setValue(data['server-fingerprint']);
				win.down('#enrollmentCode').setValue(encodeEnrollment(win.down('#serverField').getValue().trim()));
				win.down('#copyEnrollment').setDisabled(false);
			    },
			    failure: function(result) {
				if (!win.destroyed) { Ext.Msg.alert(gettext('Error'), result.htmlStatus); }
			    },
			    callback: function() {
				if (win.destroyed) { return; }
				win.down('#regenerateEnrollment').setDisabled(false);
				win.down('#revokeExistingKeys').setDisabled(false);
			    },
			});
		    };

		    view.enrollmentWindow = Ext.create('Ext.window.Window', {
			title: gettext('Enroll Agent'),
			modal: true,
			width: 760,
			height: 640,
			layout: 'fit',
			items: [{
			    xtype: 'form',
			    scrollable: true,
			    bodyPadding: 15,
			    defaults: {
				anchor: '100%',
				labelWidth: 150,
			    },
			    items: [{
				xtype: 'proxmoxcheckbox',
				itemId: 'revokeExistingKeys',
				boxLabel: gettext('Revoke existing keys'),
				checked: false,
				listeners: { change: function(field) {
				    let win = field.up('window');
				    if (win && win.down('#enrollmentCode')) { refreshEnrollment(win); }
				} },
			    }, {
				xtype: 'displayfield',
				value: gettext('Previous generated keys keep backup access unless revocation is selected. Revocation happens when this code is used, not when it is generated. The newest enrollment controls agent jobs; manual API keys are unaffected.'),
			    }, {
				xtype: 'proxmoxKVComboBox',
				itemId: 'storageMode',
				fieldLabel: gettext('Storage credentials'),
				value: 'auto',
				comboItems: [['auto', gettext('Automatic device key')], ['custom', gettext('Existing PBS API key')]],
				listeners: { change: function(field, value) {
				    let win = field.up('window');
				    if (!win || !win.down('#enrollmentCode')) { return; }
				    win.down('#storageAuthID').setDisabled(value !== 'custom');
				    win.down('#storageSecret').setDisabled(value !== 'custom');
				    win.down('#enrollmentCode').setValue(encodeEnrollment(win.down('#serverField').getValue().trim()));
				} },
			    }, {
				xtype: 'textfield', itemId: 'storageAuthID', disabled: true, allowBlank: false,
				fieldLabel: gettext('API token ID'), emptyText: 'user@realm!token',
				listeners: { change: function(field) {
				    let win = field.up('window');
				    if (!win || !win.down('#enrollmentCode')) { return; }
				    win.down('#enrollmentCode').setValue(encodeEnrollment(win.down('#serverField').getValue().trim()));
				} },
			    }, {
				xtype: 'textfield', itemId: 'storageSecret', inputType: 'password', disabled: true, allowBlank: false,
				fieldLabel: gettext('API token secret'),
				listeners: { change: function(field) {
				    let win = field.up('window');
				    if (!win || !win.down('#enrollmentCode')) { return; }
				    win.down('#enrollmentCode').setValue(encodeEnrollment(win.down('#serverField').getValue().trim()));
				} },
			    }, {
				xtype: 'displayfield',
				value: gettext('Automatic keys remain isolated per device. An existing storage key follows its PBS permissions. Enrollment codes containing a storage key are sensitive.'),
			    }, {
				xtype: 'displayfield',
				fieldLabel: gettext('ID'),
				value: view.host,
			    }, {
				xtype: 'textfield',
				itemId: 'serverField',
				fieldLabel: gettext('Server / Host'),
				value: origin,
				allowBlank: false,
				listeners: {
				    change: function(field, value) {
					field.up('form').down('#enrollmentCode').setValue(encodeEnrollment(value.trim()));
				    },
				},
			    }, {
				xtype: 'displayfield',
				fieldLabel: gettext('Token'),
				itemId: 'tokenField',
				value: data.token,
			    }, {
				xtype: 'displayfield',
				fieldLabel: gettext('Fingerprint'),
				itemId: 'fingerprintField',
				value: data['server-fingerprint'],
			    }, {
				xtype: 'displayfield',
				fieldLabel: gettext('Run'),
				value: '&lt;agent_executable&gt; enroll &lt;enrollment_code&gt;',
			    }, {
				xtype: 'displayfield',
				itemId: 'enrollmentCode',
				fieldLabel: gettext('Enrollment Code'),
				value: initialCode,
				fieldStyle: 'font-family: monospace; word-break: break-all; white-space: normal',
			    }],
			}],
			buttons: [{
			    text: gettext('Regenerate code'),
			    itemId: 'regenerateEnrollment',
			    handler: function(button) { refreshEnrollment(button.up('window')); },
			}, {
			    text: gettext('Copy Enrollment Code'),
			    itemId: 'copyEnrollment',
			    iconCls: 'fa fa-clipboard',
			    handler: function(button) {
				let win = button.up('window');
				if (!win.down('form').isValid()) { return; }
				let area = win.down('#enrollmentCode');
				let value = area.getValue();
				if (!value) { return; }
				if (navigator.clipboard) {
				    navigator.clipboard.writeText(value).catch(() => {
					let selection = window.getSelection();
					let range = document.createRange();
					range.selectNodeContents(area.el.dom);
					selection.removeAllRanges();
					selection.addRange(range);
				    });
				} else {
				    let selection = window.getSelection();
				    let range = document.createRange();
				    range.selectNodeContents(area.el.dom);
				    selection.removeAllRanges();
				    selection.addRange(range);
				    document.execCommand('copy');
				}
			    },
			}, {
			    text: gettext('Close'),
			    handler: function(button) { button.up('window').close(); },
			}],
			listeners: {
			    destroy: function() { view.enrollmentWindow = undefined; },
			    afterrender: function(win) {
				win.down('#serverField').focus(true, 100);
			    },
			},
		    }).show();
		},
		failure: function(response) {
		    Ext.Msg.alert(gettext('Error'), response.htmlStatus);
		},
	    });
	},
    },

    tbar: [{
	text: gettext('Generate enrollment token'),
	iconCls: 'fa fa-key',
	handler: 'generateEnrollmentToken',
    }],

    viewConfig: {
	enableTextSelection: true,
    },

    columns: [{
	text: gettext('Name'),
	dataIndex: 'label',
	width: 220,
    }, {
	text: gettext('Value'),
	dataIndex: 'value',
	flex: 1,
	renderer: function(value, metaData, record) {
	    switch (record.get('key')) {
		case 'online':
		    return Ext.isBoolean(value) ? Proxmox.Utils.format_boolean(value) : '-';
		case 'last-seen':
		    return value ? Proxmox.Utils.render_timestamp(value) : '-';
		case 'capabilities':
		    return Ext.isArray(value) ? value.join(', ') : '-';
		default:
		    return value === undefined || value === null || value === '' ? '-' : value;
	    }
	},
    }],

    reload: function() {
	let me = this;
	Proxmox.Utils.API2Request({
	    url: `/admin/host/${encodeURIComponent(me.host)}`,
	    success: function(response) {
		let host = response.result.data || {};
		let status = host.status || {};
		let getValue = key => Object.hasOwn(host, key) ? host[key] : status[key];
		me.getStore().loadData([
		    { key: 'name', label: gettext('Host ID'), value: getValue('name') },
		    { key: 'hostname', label: gettext('Hostname'), value: getValue('hostname') },
		    { key: 'online', label: gettext('Connected'), value: getValue('online') },
		    { key: 'operating-system', label: gettext('Operating System'), value: getValue('operating-system') },
		    { key: 'architecture', label: gettext('Architecture'), value: getValue('architecture') },
		    { key: 'last-seen', label: gettext('Last Seen'), value: getValue('last-seen') },
		    { key: 'agent-version', label: gettext('Version'), value: getValue('agent-version') },
		    { key: 'capabilities', label: gettext('Capabilities'), value: getValue('capabilities') },
		]);
	    },
	    failure: function(response) {
		Ext.Msg.alert(gettext('Error'), response.htmlStatus);
	    },
	});
    },

    listeners: {
	activate: function(view) {
	    view.reload();
	    if (!view.refreshTask) {
		view.refreshTask = Ext.TaskManager.start({
		    run: () => {
			if (view.isVisible(true)) {
			    view.reload();
			}
		    },
		    interval: 15 * 1000,
		});
	    }
	},
	destroy: function(view) {
	    if (view.refreshTask) {
		Ext.TaskManager.stop(view.refreshTask);
		view.refreshTask = undefined;
	    }
	},
    },

    store: {
	fields: ['key', 'label', 'value'],
	data: [],
    },
});
