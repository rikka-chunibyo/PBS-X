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
	me.callParent();
    },

    updateValues: function(values) {
	let status = values.status || {};
	values.hostname = status.hostname;
	values['operating-system'] = status['operating-system'];
	values['os-version'] = status['os-version'];
	values.architecture = status.architecture;
	values['agent-version'] = status['agent-version'];
	values['last-seen'] = status['last-seen'];
	this.callParent([values]);
    },
});
