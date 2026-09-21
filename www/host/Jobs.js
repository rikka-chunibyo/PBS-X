Ext.define('pbsx-host-backup-jobs', {
    extend: 'Ext.data.Model',
    fields: [
	'id', 'host', 'store', 'ns', 'sources', 'excludes', 'schedule', 'snapshot-mode',
	'change-detection-mode', 'enable', 'comment',
    ],
    idProperty: 'id',
});

Ext.define('PBS.HostBackupJobView', {
    extend: 'Ext.grid.GridPanel',
    alias: 'widget.pbsHostBackupJobView',
    mixins: ['Proxmox.Mixin.CBind'],

    title: gettext('Jobs'),
    iconCls: 'fa fa-clock-o',
    stateful: true,
    stateId: 'pbsx-host-backup-jobs',

    controller: {
	xclass: 'Ext.app.ViewController',

	addJob: function() {
	    let view = this.getView();
	    Ext.create('PBS.window.HostBackupJobEdit', {
		host: view.host,
		listeners: { destroy: () => this.reload() },
	    }).show();
	},

	runJob: function() {
	    let me = this;
	    let view = me.getView();
	    let selection = view.getSelection();
	    if (selection.length < 1) {
		return;
	    }

	    let id = selection[0].data.id;
	    Proxmox.Utils.API2Request({
		method: 'POST',
		url: `/admin/host/${encodeURIComponent(view.host)}/backup`,
		params: { id },
		success: function(response) {
		    let runId = response.result.data['run-id'];
		    Ext.Msg.show({
			title: gettext('Backup queued'),
			icon: Ext.Msg.INFO,
			message: Ext.String.format(
			    gettext('Run {0} was queued for this host.'),
			    Ext.String.htmlEncode(runId),
			),
			buttons: Ext.Msg.OK,
		    });
		},
		failure: function(response) {
		    Ext.Msg.alert(gettext('Error'), response.htmlStatus);
		},
	    });
	},

	editJob: function() {
	    let view = this.getView();
	    let selection = view.getSelection();
	    if (selection.length < 1) {
		return;
	    }
	    Ext.create('PBS.window.HostBackupJobEdit', {
		host: view.host,
		id: selection[0].data.id,
		listeners: { destroy: () => this.reload() },
	    }).show();
	},

	reload: function() {
	    this.getView().getStore().load();
	},
    },

    listeners: {
	activate: 'reload',
	itemdblclick: 'editJob',
    },

    tbar: [
	{
	    text: gettext('Add'),
	    iconCls: 'fa fa-plus',
	    handler: 'addJob',
	},
	{
	    xtype: 'proxmoxButton',
	    text: gettext('Edit'),
	    disabled: true,
	    handler: 'editJob',
	},
	{
	    xtype: 'proxmoxStdRemoveButton',
	    baseurl: '/config/host-backup',
	    callback: 'reload',
	},
	'-',
	{
	    xtype: 'proxmoxButton',
	    text: gettext('Run now'),
	    iconCls: 'fa fa-play',
	    disabled: true,
	    handler: 'runJob',
	},
    ],

    columns: [
	{ header: gettext('Job ID'), dataIndex: 'id', width: 160 },
	{ header: gettext('Datastore'), dataIndex: 'store', width: 140 },
	{ header: gettext('Namespace'), dataIndex: 'ns', width: 140, renderer: value => value || '/' },
	{ header: gettext('Schedule'), dataIndex: 'schedule', width: 160, renderer: value => value || gettext('Manual') },
	{
	    header: gettext('Sources'),
	    dataIndex: 'sources',
	    flex: 1,
	    renderer: value => Ext.String.htmlEncode((value || []).join(', ')),
	},
	{ header: gettext('Snapshot'), dataIndex: 'snapshot-mode', width: 110 },
	{
	    header: gettext('Enabled'),
	    dataIndex: 'enable',
	    width: 90,
	    renderer: Proxmox.Utils.format_boolean,
	},
    ],

    initComponent: function() {
	let me = this;
	me.store = Ext.create('Ext.data.Store', {
	    model: 'pbsx-host-backup-jobs',
	    proxy: {
		type: 'proxmox',
		url: '/api2/json/config/host-backup',
		extraParams: { host: me.host },
	    },
	    sorters: ['id'],
	});
	me.callParent();
    },
});
