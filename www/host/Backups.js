Ext.define('pbsx-host-snapshot', {
    extend: 'Ext.data.Model',
    fields: ['store', 'ns', 'backup-time', 'comment', 'size', 'protected'],
});

Ext.define('PBS.HostBackupSnapshotView', {
    extend: 'Ext.grid.GridPanel',
    alias: 'widget.pbsHostBackupSnapshotView',

    title: gettext('Backups'),
    iconCls: 'fa fa-archive',

    listeners: {
	activate: function(view) { view.getStore().load(); },
    },

    columns: [
	{
	    header: gettext('Backup Time'),
	    dataIndex: 'backup-time',
	    width: 180,
	    renderer: Proxmox.Utils.render_timestamp,
	},
	{ header: gettext('Datastore'), dataIndex: 'store', width: 150 },
	{ header: gettext('Namespace'), dataIndex: 'ns', width: 150, renderer: value => value || '/' },
	{
	    header: gettext('Size'),
	    dataIndex: 'size',
	    width: 120,
	    renderer: value => value === undefined ? '-' : Proxmox.Utils.format_size(value),
	},
	{
	    header: gettext('Protected'),
	    dataIndex: 'protected',
	    width: 100,
	    renderer: Proxmox.Utils.format_boolean,
	},
	{ header: gettext('Comment'), dataIndex: 'comment', flex: 1, renderer: Ext.String.htmlEncode },
    ],

    initComponent: function() {
	let me = this;
	me.store = Ext.create('Ext.data.Store', {
	    model: 'pbsx-host-snapshot',
	    proxy: {
		type: 'proxmox',
		url: `/api2/json/admin/host/${encodeURIComponent(me.host)}/snapshots`,
	    },
	    sorters: [{ property: 'backup-time', direction: 'DESC' }],
	});
	me.callParent();
    },
});
