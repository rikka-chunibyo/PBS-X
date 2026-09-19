Ext.define('pbsx-host-backup-runs', {
    extend: 'Ext.data.Model',
    fields: [
	'run-id', 'job-id', 'host-id', 'state', 'store', 'ns', 'requested-at', 'backup-time',
	'started-at', 'updated-at', 'finished-at', 'success', 'error', 'stage',
	'processed-bytes', 'uploaded-bytes', 'processed-chunks', 'uploaded-chunks',
	'files', 'directories', 'logical-bytes', 'duration-ms',
    ],
    idProperty: 'run-id',
});

Ext.define('PBS.HostBackupRunView', {
    extend: 'Ext.grid.GridPanel',
    alias: 'widget.pbsHostBackupRunView',

    title: gettext('Runs'),
    iconCls: 'fa fa-tasks',
    stateful: true,
    stateId: 'pbsx-host-backup-runs',

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
	{ header: gettext('Job ID'), dataIndex: 'job-id', width: 150 },
	{ header: gettext('State'), dataIndex: 'state', width: 90 },
	{ header: gettext('Stage'), dataIndex: 'stage', width: 120, renderer: value => value || '-' },
	{
	    header: gettext('Processed'),
	    dataIndex: 'processed-bytes',
	    width: 110,
	    renderer: value => Proxmox.Utils.format_size(value || 0),
	},
	{
	    header: gettext('Uploaded'),
	    dataIndex: 'uploaded-bytes',
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
	    header: gettext('Error'),
	    dataIndex: 'error',
	    flex: 1,
	    renderer: value => value ? Ext.String.htmlEncode(value) : '',
	},
    ],

    initComponent: function() {
	let me = this;
	me.store = Ext.create('Ext.data.Store', {
	    model: 'pbsx-host-backup-runs',
	    proxy: {
		type: 'proxmox',
		url: `/api2/json/admin/host/${encodeURIComponent(me.host)}/runs`,
	    },
	    sorters: [{ property: 'backup-time', direction: 'DESC' }],
	});
	me.callParent();
    },
});
