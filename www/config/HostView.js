Ext.define('pbsx-hosts', {
    extend: 'Ext.data.Model',
    fields: [
	'name',
	'enable',
	'comment',
	'online',
	'status',
	{
	    name: 'hostname',
	    calculate: data => data.status?.hostname || '',
	},
	{
	    name: 'operating-system',
	    calculate: data => data.status?.['operating-system'] || '',
	},
	{
	    name: 'os-version',
	    calculate: data => data.status?.['os-version'] || '',
	},
	{
	    name: 'architecture',
	    calculate: data => data.status?.architecture || '',
	},
	{
	    name: 'agent-version',
	    calculate: data => data.status?.['agent-version'] || '',
	},
	{
	    name: 'last-seen',
	    type: 'integer',
	    calculate: data => data.status?.['last-seen'],
	},
    ],
    idProperty: 'name',
    proxy: {
	type: 'proxmox',
	url: '/api2/json/admin/host',
    },
});

Ext.define('PBS.config.HostView', {
    extend: 'Ext.grid.GridPanel',
    alias: 'widget.pbsHostView',

    stateful: true,
    stateId: 'grid-pbsx-hosts',
    title: gettext('Hosts'),

    controller: {
	xclass: 'Ext.app.ViewController',

	addHost: function() {
	    let me = this;
	    Ext.create('PBS.window.HostEdit', {
		listeners: {
		    destroy: function() {
			me.reload();
		    },
		},
	    }).show();
	},

	openHost: function() {
	    let view = this.getView();
	    let selection = view.getSelection();
	    if (selection.length < 1) {
		return;
	    }
	    let mainview = view.up('mainview');
	    mainview.getController().redirectTo(`Host-${encodeURIComponent(selection[0].data.name)}`);
	},

	editHost: function() {
	    let me = this;
	    let view = me.getView();
	    let selection = view.getSelection();
	    if (selection.length < 1) {
		return;
	    }

	    Ext.create('PBS.window.HostEdit', {
		name: selection[0].data.name,
		listeners: {
		    destroy: function() {
			me.reload();
		    },
		},
	    }).show();
	},

	reload: function() {
	    this.getView().getStore().rstore.load();
	},

	init: function(view) {
	    Proxmox.Utils.monStoreErrors(view, view.getStore().rstore);
	},
    },

    listeners: {
	activate: 'reload',
	itemdblclick: 'openHost',
    },

    store: {
	type: 'diff',
	autoDestroy: true,
	autoDestroyRstore: true,
	sorters: 'name',
	rstore: {
	    type: 'update',
	    storeid: 'pbsx-hosts',
	    model: 'pbsx-hosts',
	    autoStart: true,
	    interval: 15000,
	},
    },

    tbar: [
	{
	    xtype: 'proxmoxButton',
	    text: gettext('Add'),
	    handler: 'addHost',
	    selModel: false,
	},
	{
	    xtype: 'proxmoxButton',
	    text: gettext('Open'),
	    iconCls: 'fa fa-folder-open',
	    handler: 'openHost',
	    disabled: true,
	},
	{
	    xtype: 'proxmoxButton',
	    text: gettext('Edit'),
	    handler: 'editHost',
	    disabled: true,
	},
	{
	    xtype: 'proxmoxStdRemoveButton',
	    baseurl: '/config/host',
	    callback: 'reload',
	},
    ],

    viewConfig: {
	trackOver: false,
    },

    columns: [
	{
	    header: gettext('Host ID'),
	    dataIndex: 'name',
	    width: 180,
	    sortable: true,
	    renderer: Ext.String.htmlEncode,
	},
	{
	    header: gettext('Status'),
	    dataIndex: 'online',
	    width: 110,
	    renderer: function(value, meta, record) {
		if (record.data.enable === false) {
		    return gettext('Disabled');
		}
		if (!record.data.status) {
		    return gettext('Never seen');
		}
		return value ? gettext('Online') : gettext('Offline');
	    },
	},
	{
	    header: gettext('Hostname'),
	    dataIndex: 'hostname',
	    width: 180,
	    renderer: Ext.String.htmlEncode,
	},
	{
	    header: gettext('OS'),
	    dataIndex: 'operating-system',
	    width: 100,
	    renderer: function(value, meta, record) {
		let version = record.data['os-version'];
		let text = value;
		if (version) {
		    text += ` (${version})`;
		}
		return Ext.String.htmlEncode(text);
	    },
	},
	{
	    header: gettext('Architecture'),
	    dataIndex: 'architecture',
	    width: 120,
	    renderer: Ext.String.htmlEncode,
	},
	{
	    header: gettext('Agent Version'),
	    dataIndex: 'agent-version',
	    width: 120,
	    renderer: Ext.String.htmlEncode,
	},
	{
	    header: gettext('Last Seen'),
	    dataIndex: 'last-seen',
	    width: 160,
	    renderer: function(value) {
		return value ? Proxmox.Utils.render_timestamp(value) : '-';
	    },
	},
	{
	    header: gettext('Comment'),
	    dataIndex: 'comment',
	    flex: 1,
	    renderer: Ext.String.htmlEncode,
	},
    ],
});
