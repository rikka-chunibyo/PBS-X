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
		    let text = [
			`PBSX_HOST_ID=${view.host}`,
			`PBSX_INIT_SERVER_URL=${origin}`,
			`PBSX_INIT_BOOTSTRAP_TOKEN=${data.token}`,
			`PBSX_INIT_SERVER_CA_FINGERPRINT=${data['server-fingerprint']}`,
		    ].join('\n');

		    Ext.create('Ext.window.Window', {
			title: gettext('Enroll Agent'),
			modal: true,
			width: 720,
			layout: 'fit',
			items: [{
			    xtype: 'textarea',
			    value: text,
			    readOnly: true,
			    grow: true,
			    fieldStyle: 'font-family: monospace',
			}],
			buttons: [{
			    text: gettext('Close'),
			    handler: function(button) { button.up('window').close(); },
			}],
			listeners: {
			    afterrender: function(win) {
				win.down('textarea').focus(true, 100);
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
