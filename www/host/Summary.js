Ext.define('PBS.HostSummary', {
    extend: 'Ext.grid.Panel',
    alias: 'widget.pbsHostSummary',

    title: gettext('Summary'),
    iconCls: 'fa fa-desktop',

    viewConfig: {
	enableTextSelection: true,
    },

    columns: [{
	text: gettext('Name'),
	dataIndex: 'label',
	width: 220,
    }, {
	text: gettext('Value'),
	dataIndex: 'value',
	flex: 1,
	renderer: function(value, metaData, record) {
	    switch (record.get('key')) {
		case 'online':
		    if (!Ext.isBoolean(value)) {
			return '-';
		    }
		    return value ? gettext('Online') : gettext('Offline');
		case 'enable':
		    return Ext.isBoolean(value) ? Proxmox.Utils.format_boolean(value) : '-';
		case 'last-seen':
		    return value ? Proxmox.Utils.render_timestamp(value) : '-';
		default:
		    return value === undefined || value === null || value === '' ? '-' : value;
	    }
	},
    }],

    reload: function() {
	let me = this;
	Proxmox.Utils.API2Request({
	    url: `/admin/host/${encodeURIComponent(me.host)}`,
	    success: function(response) {
		let host = response.result.data || {};
		let status = host.status || {};
		let getValue = key => Object.hasOwn(host, key) ? host[key] : status[key];
		me.getStore().loadData([
		    { key: 'name', label: gettext('Host ID'), value: getValue('name') },
		    { key: 'online', label: gettext('Status'), value: getValue('online') },
		    { key: 'enable', label: gettext('Enabled'), value: getValue('enable') },
		    { key: 'hostname', label: gettext('Hostname'), value: getValue('hostname') },
		    { key: 'operating-system', label: gettext('Operating System'), value: getValue('operating-system') },
		    { key: 'os-version', label: gettext('OS Version'), value: getValue('os-version') },
		    { key: 'architecture', label: gettext('Architecture'), value: getValue('architecture') },
		    { key: 'agent-version', label: gettext('Agent Version'), value: getValue('agent-version') },
		    { key: 'last-seen', label: gettext('Last Seen'), value: getValue('last-seen') },
		    { key: 'comment', label: gettext('Comment'), value: getValue('comment') },
		]);
	    },
	    failure: function(response) {
		Ext.Msg.alert(gettext('Error'), response.htmlStatus);
	    },
	});
    },

    listeners: {
	activate: function(view) {
	    view.reload();
	    if (!view.refreshTask) {
		view.refreshTask = Ext.TaskManager.start({
		    run: () => {
			if (view.isVisible(true)) {
			    view.reload();
			}
		    },
		    interval: 15 * 1000,
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

    store: {
	fields: ['key', 'label', 'value'],
	data: [],
    },
});
