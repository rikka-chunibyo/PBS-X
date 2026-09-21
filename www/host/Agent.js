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

    rows: {
	name: { header: gettext('Host ID'), required: true },
	hostname: { header: gettext('Hostname') },
	online: { header: gettext('Connected'), renderer: Proxmox.Utils.format_boolean },
	'operating-system': { header: gettext('Operating System') },
	architecture: { header: gettext('Architecture') },
	'last-seen': { header: gettext('Last Seen'), renderer: Proxmox.Utils.render_timestamp },
	'agent-version': { header: gettext('Version') },
	capabilities: { header: gettext('Capabilities'), renderer: value => Ext.isArray(value) ? value.join(', ') : '' },
    },

    initComponent: function() {
	let me = this;
	me.url = `/api2/json/admin/host/${encodeURIComponent(me.host)}`;
	me.rstore = Ext.create('Proxmox.data.ObjectStore', {
	    interval: me.interval,
	    rows: me.rows,
	    proxy: {
		type: 'proxmox',
		url: me.url,
		reader: {
		    type: 'pbsxhostobject',
		    rows: me.rows,
		    rootProperty: 'data',
		},
	    },
	});
	me.callParent();
    },
});
