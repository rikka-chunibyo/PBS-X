Ext.define('PBS.HostAgentView', {
    extend: 'Ext.grid.Panel',
    alias: 'widget.pbsHostAgentView',

    title: gettext('Agent'),
    iconCls: 'fa fa-exchange',

    controller: {
	xclass: 'Ext.app.ViewController',

	generateEnrollmentToken: function() {
	    let view = this.getView();
	    Proxmox.Utils.API2Request({
		method: 'POST',
		url: `/config/host/${encodeURIComponent(view.host)}/enrollment-token`,
		success: function(response) {
		    let data = response.result.data;
		    let origin = window.location.origin;
		    let encodeEnrollment = function(server) {
			let payload = JSON.stringify({
			    v: 1,
			    host_id: view.host,
			    server_url: server,
			    bootstrap_token: data.token,
			    server_ca_fingerprint: data['server-fingerprint'],
			});
			let binary = '';
			for (let byte of new TextEncoder().encode(payload)) {
			    binary += String.fromCharCode(byte);
			}
			return `pbsx1.${btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')}`;
		    };
		    let initialCode = encodeEnrollment(origin);

		    Ext.create('Ext.window.Window', {
			title: gettext('Enroll Agent'),
			modal: true,
			width: 760,
			height: 430,
			layout: 'fit',
			items: [{
			    xtype: 'form',
			    bodyPadding: 15,
			    defaults: {
				anchor: '100%',
				labelWidth: 150,
			    },
			    items: [{
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
				value: data.token,
			    }, {
				xtype: 'displayfield',
				fieldLabel: gettext('Fingerprint'),
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
			    text: gettext('Copy Enrollment Code'),
			    iconCls: 'fa fa-clipboard',
			    handler: function(button) {
				let win = button.up('window');
				let area = win.down('#enrollmentCode');
				let value = area.getValue();
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
	    url: `/api2/json/admin/host/${encodeURIComponent(me.host)}`,
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
