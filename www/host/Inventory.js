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
	    url: `/api2/json/admin/host/${encodeURIComponent(me.host)}`,
	    waitMsgTarget: me,
	    success: function(response) {
		let status = response.result.data.status || {};
		me.down('#diskgrid').getStore().loadData(status.disks || []);
		me.down('#fsgrid').getStore().loadData(status.filesystems || []);
	    },
	});
    },

    items: [
	{
	    xtype: 'grid',
	    itemId: 'diskgrid',
	    title: gettext('Disks'),
	    flex: 1,
	    store: { fields: ['name', 'size', 'model', 'serial', 'rotational'], data: [] },
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
	    store: { fields: ['mountpoint', 'file-system', 'source', 'total', 'available'], data: [] },
	    columns: [
		{ text: gettext('Mount / Volume'), dataIndex: 'mountpoint', width: 190 },
		{ text: gettext('Filesystem'), dataIndex: 'file-system', width: 120 },
		{ text: gettext('Source'), dataIndex: 'source', flex: 1 },
		{ text: gettext('Total'), dataIndex: 'total', width: 120, renderer: value => value ? Proxmox.Utils.format_size(value) : '-' },
		{ text: gettext('Available'), dataIndex: 'available', width: 120, renderer: value => value ? Proxmox.Utils.format_size(value) : '-' },
	    ],
	},
    ],
});
