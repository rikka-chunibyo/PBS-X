Ext.define('PBS.HostAgentView', {
    extend: 'Proxmox.grid.ObjectGrid',
    alias: 'widget.pbsHostAgentView',

    title: gettext('Agent'),
    iconCls: 'fa fa-exchange',
    interval: 15 * 1000,

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
		    let values = {
			PBSX_HOST_ID: view.host,
			PBSX_INIT_SERVER_URL: origin,
			PBSX_INIT_BOOTSTRAP_TOKEN: data.token,
			PBSX_INIT_SERVER_CA_FINGERPRINT: data['server-fingerprint'],
			GODEBUG: 'fips140=on',
			PBSX_STDOUT_ONLY: 'true',
		    };
		    let powerShell = Object.entries(values).map(([key, value]) => {
			value = String(value).replace(/'/g, "''");
			return `$env:${key} = '${value}'`;
		    });
		    powerShell.push("& '.\\pbsx-host-agent-windows-amd64.exe'");
		    let commandPrompt = Object.entries(values).map(([key, value]) => {
			return `set "${key}=${value}"`;
		    });
		    commandPrompt.push('pbsx-host-agent-windows-amd64.exe');
		    let raw = Object.entries(values).map(([key, value]) => `${key}=${value}`);
		    let posixShell = Object.entries(values).map(([key, value]) => {
			value = String(value).replace(/'/g, "'\\''");
			return `export ${key}='${value}'`;
		    });
		    posixShell.push('./pbsx-host-agent-linux-amd64');

		    let textArea = function(title, value) {
			return {
			    xtype: 'textarea',
			    title,
			    value: value.join('\n'),
			    readOnly: true,
			    fieldStyle: 'font-family: monospace',
			};
		    };

		    Ext.create('Ext.window.Window', {
			title: gettext('Enroll Agent'),
			modal: true,
			width: 820,
			height: 440,
			layout: 'fit',
			items: [{
			    xtype: 'tabpanel',
			    items: [
				textArea(gettext('PowerShell'), powerShell),
				textArea(gettext('CMD'), commandPrompt),
				textArea(gettext('Bash'), posixShell),
				textArea(gettext('Raw'), raw),
			    ],
			}],
			buttons: [{
			    text: gettext('Copy'),
			    handler: function(button) {
				let win = button.up('window');
				let area = win.down('tabpanel').getActiveTab();
				let value = area.getValue();
				if (navigator.clipboard) {
				    navigator.clipboard.writeText(value).catch(() => area.selectText());
				} else {
				    area.selectText();
				    document.execCommand('copy');
				}
			    },
			}, {
			    text: gettext('Close'),
			    handler: function(button) { button.up('window').close(); },
			}],
			listeners: {
			    afterrender: function(win) {
				win.down('tabpanel').getActiveTab().focus(true, 100);
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

    rows: {
	online: { header: gettext('Connected'), renderer: Proxmox.Utils.format_boolean },
	'last-seen': { header: gettext('Last Seen'), renderer: Proxmox.Utils.render_timestamp },
	'agent-version': { header: gettext('Version') },
	capabilities: { header: gettext('Capabilities'), renderer: value => Ext.isArray(value) ? value.join(', ') : '' },
    },

    initComponent: function() {
	let me = this;
	me.url = `/api2/json/admin/host/${encodeURIComponent(me.host)}`;
	me.callParent();
    },

    updateValues: function(values) {
	let status = values.status || {};
	values['last-seen'] = status['last-seen'];
	values['agent-version'] = status['agent-version'];
	values.capabilities = status.capabilities || [];
	this.callParent([values]);
    },
});
