Ext.define('pbsx-host-restore-runs', {
    extend: 'Ext.data.Model',
    fields: [
	'run-id', 'host-id', 'state', 'store', 'ns', 'backup-time', 'sources', 'destination', 'mode',
	'requested-at', 'started-at', 'updated-at', 'finished-at', 'success', 'error', 'stage',
	'processed-bytes', 'files', 'duration-ms',
    ],
    idProperty: 'run-id',
});

Ext.define('PBS.HostRestoreRunView', {
    extend: 'Ext.grid.GridPanel',
    alias: 'widget.pbsHostRestoreRunView',

    title: gettext('Restores'),
    iconCls: 'fa fa-undo',
    stateful: true,
    stateId: 'pbsx-host-restore-runs',

    controller: {
	xclass: 'Ext.app.ViewController',

	reload: function() {
	    this.getView().getStore().load();
	},
    },

    listeners: {
	activate: function(view) {
	    view.getStore().load();
	    if (!view.refreshTask) {
		view.refreshTask = Ext.TaskManager.start({
		    run: () => {
			if (view.isVisible(true)) {
			    view.getStore().load();
			}
		    },
		    interval: 5000,
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

    tbar: [
	{
	    text: gettext('Refresh'),
	    iconCls: 'fa fa-refresh',
	    handler: 'reload',
	},
    ],

    columns: [
	{ header: gettext('Backup Time'), dataIndex: 'backup-time', width: 165, renderer: Proxmox.Utils.render_timestamp },
	{ header: gettext('Datastore'), dataIndex: 'store', width: 120 },
	{ header: gettext('Namespace'), dataIndex: 'ns', width: 110, renderer: value => value || '/' },
	{ header: gettext('State'), dataIndex: 'state', width: 90 },
	{ header: gettext('Stage'), dataIndex: 'stage', width: 110, renderer: value => value || '-' },
	{
	    header: gettext('Processed'),
	    dataIndex: 'processed-bytes',
	    width: 110,
	    renderer: value => Proxmox.Utils.format_size(value || 0),
	},
	{ header: gettext('Files'), dataIndex: 'files', width: 80 },
	{
	    header: gettext('Duration'),
	    dataIndex: 'duration-ms',
	    width: 100,
	    renderer: value => value ? Proxmox.Utils.format_duration_human(value / 1000) : '-',
	},
	{
	    header: gettext('Result'),
	    dataIndex: 'success',
	    width: 90,
	    renderer: function(value, meta, record) {
		if (record.data.state !== 'finished') return '-';
		return value ? gettext('OK') : gettext('Failed');
	    },
	},
	{
	    header: gettext('Destination'),
	    dataIndex: 'destination',
	    width: 180,
	    renderer: Ext.String.htmlEncode,
	},
	{
	    header: gettext('Error'),
	    dataIndex: 'error',
	    flex: 1,
	    renderer: value => value ? Ext.String.htmlEncode(value) : '',
	},
    ],

    initComponent: function() {
	let me = this;
	me.store = Ext.create('Ext.data.Store', {
	    model: 'pbsx-host-restore-runs',
	    proxy: {
		type: 'proxmox',
		url: `/api2/json/admin/host/${encodeURIComponent(me.host)}/restore-runs`,
	    },
	    sorters: [{ property: 'requested-at', direction: 'DESC' }],
	});
	me.callParent();
    },
});
