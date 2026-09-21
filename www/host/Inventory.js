Ext.define('PBS.HostInventoryView', {
    extend: 'Ext.panel.Panel',
    alias: 'widget.pbsHostInventoryView',

    title: gettext('Disks / Volumes'),
    iconCls: 'fa fa-hdd-o',
    layout: { type: 'vbox', align: 'stretch' },

    listeners: {
	activate: function(view) { view.reload(); },
    },

    reload: function() {
	let me = this;
	Proxmox.Utils.API2Request({
	    url: `/admin/host/${encodeURIComponent(me.host)}`,
	    success: function(response) {
		let host = response.result.data || {};
		let status = host.status || {};
		me.diskStore.loadData(Ext.isArray(status.disks) ? status.disks : []);
		me.filesystemStore.loadData(Ext.isArray(status.filesystems) ? status.filesystems : []);
	    },
	    failure: function(response) {
		Ext.Msg.alert(gettext('Error'), response.htmlStatus);
	    },
	});
    },

    initComponent: function() {
	let me = this;
	me.diskStore = Ext.create('Ext.data.Store', {
	    fields: ['name', 'size', 'model', 'serial', 'rotational'],
	    data: [],
	});
	me.filesystemStore = Ext.create('Ext.data.Store', {
	    fields: ['mountpoint', 'file-system', 'source', 'total', 'available'],
	    data: [],
	});
	me.items = [{
	    xtype: 'grid',
	    itemId: 'diskgrid',
	    title: gettext('Disks'),
	    flex: 1,
	    store: me.diskStore,
	    columns: [
		{ text: gettext('Device'), dataIndex: 'name', width: 180 },
		{ text: gettext('Model'), dataIndex: 'model', flex: 1 },
		{ text: gettext('Serial'), dataIndex: 'serial', width: 180 },
		{ text: gettext('Size'), dataIndex: 'size', width: 120, renderer: value => value ? Proxmox.Utils.format_size(value) : '-' },
	    ],
	},
	{
	    xtype: 'grid',
	    itemId: 'fsgrid',
	    title: gettext('Filesystems / Volumes'),
	    flex: 1,
	    store: me.filesystemStore,
	    columns: [
		{ text: gettext('Mount / Volume'), dataIndex: 'mountpoint', width: 190 },
		{ text: gettext('Filesystem'), dataIndex: 'file-system', width: 120 },
		{ text: gettext('Source'), dataIndex: 'source', flex: 1 },
		{ text: gettext('Total'), dataIndex: 'total', width: 120, renderer: value => value ? Proxmox.Utils.format_size(value) : '-' },
		{ text: gettext('Available'), dataIndex: 'available', width: 120, renderer: value => value ? Proxmox.Utils.format_size(value) : '-' },
	    ],
	}];
	me.callParent();
    },
});
