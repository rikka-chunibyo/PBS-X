Ext.define('PBS.HostAgentView', {
    extend: 'Proxmox.grid.ObjectGrid',
    alias: 'widget.pbsHostAgentView',

    title: gettext('Agent'),
    iconCls: 'fa fa-exchange',
    interval: 15 * 1000,

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
