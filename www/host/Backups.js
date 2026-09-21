Ext.define('pbsx-host-snapshot', {
    extend: 'Ext.data.Model',
    fields: ['store', 'ns', 'backup-time', 'comment', 'size', 'protected'],
});

Ext.define('PBS.window.HostRestore', {
    extend: 'Proxmox.window.Edit',
    alias: 'widget.pbsHostRestoreWindow',

    subject: gettext('Restore Host Backup'),
    width: 620,
    method: 'POST',

    initComponent: function() {
	let me = this;
	me.url = `/api2/extjs/admin/host/${encodeURIComponent(me.host)}/restore`;
	me.items = {
	    xtype: 'inputpanel',
	    onGetValues: function(values) {
		if (typeof values.sources === 'string') {
		    values.sources = values.sources
			.split(/\r?\n/)
			.map(value => value.trim())
			.filter(value => value.length > 0);
		}
		return values;
	    },
	    column1: [
		{ xtype: 'displayfield', fieldLabel: gettext('Datastore'), value: me.store },
		{ xtype: 'displayfield', fieldLabel: gettext('Namespace'), value: me.ns || '/' },
		{
		    xtype: 'displayfield',
		    fieldLabel: gettext('Backup Time'),
		    value: Proxmox.Utils.render_timestamp(me.backupTime),
		},
		{ xtype: 'hiddenfield', name: 'store', value: me.store },
		{ xtype: 'hiddenfield', name: 'ns', value: me.ns || '' },
		{ xtype: 'hiddenfield', name: 'backup-time', value: me.backupTime },
	    ],
	    column2: [
		{
		    xtype: 'proxmoxKVComboBox',
		    fieldLabel: gettext('Metadata'),
		    name: 'mode',
		    value: 'normal',
		    comboItems: [
			['normal', gettext('Restore metadata')],
			['noattr', gettext('Content only')],
		    ],
		},
	    ],
	    columnB: [
		{
		    xtype: 'textarea',
		    name: 'sources',
		    fieldLabel: gettext('Paths'),
		    value: '/',
		    allowBlank: false,
		    height: 90,
		    emptyText: gettext('One archive path per line. Use / for the complete backup.'),
		},
		{
		    xtype: 'proxmoxtextfield',
		    name: 'destination',
		    fieldLabel: gettext('Destination'),
		    allowBlank: false,
		    emptyText: gettext('Directory on the protected host'),
		},
	    ],
	};
	me.callParent();
    },
});

Ext.define('PBS.HostBackupSnapshotView', {
    extend: 'Ext.grid.GridPanel',
    alias: 'widget.pbsHostBackupSnapshotView',

    title: gettext('Backups'),
    iconCls: 'fa fa-archive',

    controller: {
	xclass: 'Ext.app.ViewController',

	restoreSnapshot: function() {
	    let view = this.getView();
	    let selection = view.getSelection();
	    if (selection.length < 1) {
		return;
	    }
	    let data = selection[0].data;
	    Ext.create('PBS.window.HostRestore', {
		host: view.host,
		store: data.store,
		ns: data.ns,
		backupTime: data['backup-time'],
		listeners: {
		    destroy: function(win) {
			if (win.result) {
			    Ext.Msg.show({
				title: gettext('Restore queued'),
				icon: Ext.Msg.INFO,
				message: gettext('The restore was queued for this host.'),
				buttons: Ext.Msg.OK,
			    });
			}
		    },
		},
	    }).show();
	},
    },

    listeners: {
	activate: function(view) { view.getStore().load(); },
    },

    tbar: [
	{
	    xtype: 'proxmoxButton',
	    text: gettext('Restore'),
	    iconCls: 'fa fa-undo',
	    disabled: true,
	    handler: 'restoreSnapshot',
	},
    ],

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
