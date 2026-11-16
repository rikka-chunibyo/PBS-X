Ext.define('pbsx-host-snapshot', {
    extend: 'Ext.data.Model',
    fields: ['store', 'ns', 'backup-id', 'backup-time', 'comment', 'size', 'protected'],
});

Ext.define('PBS.HostBackupBrowser', {
    singleton: true,

    // Legacy catalogs include the archive name; split-PXAR catalogs expose the
    // archive root directly. Native restore reads only root.{m,}pxar.didx.
    restorePath: function(encoded, archive) {
	let bytes = Uint8Array.from(atob(encoded), value => value.charCodeAt(0));
	let path = new TextDecoder('utf-8', { fatal: true }).decode(bytes);
	if (archive === 'root.pxar.didx') {
	    let prefix = '/root.pxar.didx';
	    if (path === prefix) {
		path = '/';
	    } else if (path.startsWith(prefix + '/')) {
		path = path.slice(prefix.length);
	    } else {
		throw new Error(gettext('Select a path inside the root backup archive.'));
	    }
	} else if (archive !== 'root.mpxar.didx') {
	    throw new Error(gettext('This archive is not supported by native host restore.'));
	}
	if (!path.startsWith('/') || /[\x00\r\n\\]/.test(path) ||
	    path.split('/').some(part => part === '..' || part === '.')) {
	    throw new Error(gettext('This filename cannot be represented safely in the restore form.'));
	}
	// The restore form uses one path per line and trims outer whitespace.
	if (path.trim() !== path) {
	    throw new Error(gettext('This filename cannot be represented safely in the restore form.'));
	}
	return path;
    },
});

Ext.define('PBS.window.HostBackupBrowser', {
    extend: 'Proxmox.window.FileBrowser',

    initComponent: function() {
	let me = this;
	let base = `/api2/json/admin/datastore/${encodeURIComponent(me.snapshot.store)}`;
	me.listURL = `${base}/catalog`;
	me.downloadURL = `${base}/pxar-file-download`;
	me.extraParams = {
	    'backup-type': 'host',
	    'backup-id': me.snapshot['backup-id'] || me.host,
	    'backup-time': me.snapshot['backup-time'],
	};
	if (me.snapshot.ns) {
	    me.extraParams.ns = me.snapshot.ns;
	}
	if (me.archive === 'root.mpxar.didx') {
	    me.extraParams['archive-name'] = me.archive;
	}
	me.enableTar = true;
	me.downloadPrefix = `host-${me.extraParams['backup-id']}-`;
	me.callParent();
	let tree = me.down('treepanel');
	let toolbar = me.addDocked({
	    xtype: 'toolbar',
	    dock: 'bottom',
	    items: [{
		text: gettext('Restore selected path'),
		iconCls: 'fa fa-undo',
		disabled: true,
		handler: function() {
		    let selection = tree.getSelection();
		    if (!selection.length) {
			return;
		    }
		    try {
			let source = PBS.HostBackupBrowser.restorePath(selection[0].data.filepath, me.archive);
			Ext.create('PBS.window.HostRestore', {
			    host: me.host,
			    store: me.snapshot.store,
			    ns: me.snapshot.ns,
			    backupID: me.extraParams['backup-id'],
			    backupTime: me.snapshot['backup-time'],
			    sources: [source],
			}).show();
		    } catch (err) {
			Ext.Msg.alert(gettext('Cannot select path'), Ext.String.htmlEncode(err.message));
		    }
		},
	    }],
	})[0];
	tree.on('selectionchange', function(_tree, selection) {
	    toolbar.items.getAt(0).setDisabled(selection.length !== 1);
	});
    },
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
		{ xtype: 'displayfield', fieldLabel: gettext('Backup ID'), value: me.backupID || me.host },
		{ xtype: 'displayfield', fieldLabel: gettext('Datastore'), value: me.store },
		{ xtype: 'displayfield', fieldLabel: gettext('Namespace'), value: me.ns || '/' },
		{
		    xtype: 'displayfield',
		    fieldLabel: gettext('Backup Time'),
		    value: Proxmox.Utils.render_timestamp(me.backupTime),
		},
		{ xtype: 'hiddenfield', name: 'store', value: me.store },
		{ xtype: 'hiddenfield', name: 'backup-id', value: me.backupID || me.host },
		{ xtype: 'hiddenfield', name: 'ns', value: me.ns || '' },
		{ xtype: 'hiddenfield', name: 'backup-time', value: me.backupTime },
	    ],
	    column2: [
		{
		    xtype: 'combobox',
		    fieldLabel: gettext('Restore to host'),
		    name: 'target-host',
		    value: me.host,
		    allowBlank: false,
		    forceSelection: true,
		    queryMode: 'local',
		    displayField: 'name',
		    valueField: 'name',
		    store: {
			autoLoad: true,
			fields: ['name'],
			proxy: { type: 'proxmox', url: '/api2/json/config/host' },
		    },
		},
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
		    value: me.sources ? me.sources.join('\n') : '/',
		    allowBlank: false,
		    height: 90,
		    emptyText: gettext('One archive path per line. Use / for the complete backup.'),
		},
		{
		    xtype: 'proxmoxtextfield',
		    name: 'destination',
		    fieldLabel: gettext('Destination'),
		    allowBlank: false,
		    emptyText: gettext('Directory on the selected destination host'),
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

	browseSnapshot: function() {
	    let view = this.getView();
	    let selection = view.getSelection();
	    if (!selection.length) {
		return;
	    }
	    let data = { ...selection[0].data };
	    let params = {
		'backup-type': 'host',
		'backup-id': data['backup-id'] || view.host,
		'backup-time': data['backup-time'],
	    };
	    if (data.ns) {
		params.ns = data.ns;
	    }
	    Proxmox.Utils.API2Request({
		url: `/api2/json/admin/datastore/${encodeURIComponent(data.store)}/files`,
		method: 'GET',
		params,
		waitMsgTarget: view,
		success: function(response) {
		    let files = response.result.data;
		    let archive = files.find(file => file.filename === 'root.mpxar.didx') ||
			files.find(file => file.filename === 'root.pxar.didx');
		    if (!archive || archive['crypt-mode'] === 'encrypt') {
			Ext.Msg.alert(gettext('Cannot browse backup'), gettext('No unencrypted root archive is available for server-side browsing.'));
			return;
		    }
		    Ext.create('PBS.window.HostBackupBrowser', {
			title: gettext('Browse host backup'),
			host: view.host,
			snapshot: data,
			archive: archive.filename,
		    }).show();
		},
		failure: response => Ext.Msg.alert(gettext('Error'), response.htmlStatus),
	    });
	},

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
		backupID: data['backup-id'] || view.host,
		backupTime: data['backup-time'],
		listeners: {
		    destroy: function(win) {
			if (win.result) {
			    Ext.Msg.show({
				title: gettext('Restore queued'),
				icon: Ext.Msg.INFO,
				message: gettext('The restore was queued for the selected destination host. Its storage API key must have access to this backup.'),
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
	    text: gettext('Browse files'),
	    iconCls: 'fa fa-folder-open-o',
	    disabled: true,
	    handler: 'browseSnapshot',
	},
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
	{ header: gettext('Backup ID'), dataIndex: 'backup-id', width: 180, renderer: Ext.String.htmlEncode },
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
