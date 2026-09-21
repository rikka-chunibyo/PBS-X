Ext.define('PBS.data.reader.HostObject', {
    extend: 'Proxmox.data.reader.JsonObject',
    alias: 'reader.pbsxhostobject',

    getResponseData: function(response) {
	let original = response.responseText;
	let payload = Ext.decode(original);
	let host = payload.data || {};
	let flattened = Ext.apply({}, host.status || {});
	Ext.apply(flattened, host);
	payload.data = flattened;
	response.responseText = Ext.encode(payload);
	try {
	    return this.callParent([response]);
	} finally {
	    response.responseText = original;
	}
    },
});

Ext.define('PBS.HostSummary', {
    extend: 'Proxmox.grid.ObjectGrid',
    alias: 'widget.pbsHostSummary',

    title: gettext('Summary'),
    iconCls: 'fa fa-desktop',
    interval: 15 * 1000,

    rows: {
	name: { header: gettext('Host ID'), required: true },
	online: {
	    header: gettext('Status'),
	    renderer: value => value ? gettext('Online') : gettext('Offline'),
	},
	enable: { header: gettext('Enabled'), renderer: Proxmox.Utils.format_boolean },
	hostname: { header: gettext('Hostname') },
	'operating-system': { header: gettext('Operating System') },
	'os-version': { header: gettext('OS Version') },
	architecture: { header: gettext('Architecture') },
	'agent-version': { header: gettext('Agent Version') },
	'last-seen': { header: gettext('Last Seen'), renderer: Proxmox.Utils.render_timestamp },
	comment: { header: gettext('Comment') },
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
