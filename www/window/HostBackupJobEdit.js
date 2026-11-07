Ext.define('PBS.window.HostBackupJobEdit', {
    extend: 'Proxmox.window.Edit',
    alias: 'widget.pbsHostBackupJobEdit',
    mixins: ['Proxmox.Mixin.CBind'],

    isAdd: true,
    subject: gettext('Host Backup Job'),
    width: 720,
    bodyPadding: 0,
    fieldDefaults: { labelWidth: 140 },

    cbindData: function(initialConfig) {
	let me = this;
	let baseurl = '/api2/extjs/config/host-backup';
	let id = initialConfig.id;

	me.isCreate = !id;
	me.url = id ? `${baseurl}/${id}` : baseurl;
	me.method = id ? 'PUT' : 'POST';
	me.autoLoad = !!id;
	return {
	    host: initialConfig.host,
	    isCreate: me.isCreate,
	    namespace: me.isCreate ? 'devices/' + initialConfig.host.match(/.{1,32}/g).join('/') : undefined,
	};
    },

    controller: {
	xclass: 'Ext.app.ViewController',
	control: {
	    'pbsDataStoreSelector': {
		change: 'storeChange',
	    },
	},

	storeChange: function(field, value) {
	    let nsSelector = this.getView().down('pbsNamespaceSelector');
	    if (nsSelector) {
		nsSelector.setDatastore(value);
	    }
	},
    },

    items: {
	xtype: 'inputpanel',
	onGetValues: function(values) {
	    let win = this.up('pbsHostBackupJobEdit');
	    if (!values.id && win.isCreate) {
		values.id = 'hb-' + Ext.data.identifier.Uuid.Global.generate().slice(0, 13);
	    }

	    if (typeof values.sources === 'string') {
		values.sources = values.sources
		    .split(/\r?\n/)
		    .map(value => value.trim())
		    .filter(value => value.length > 0);
	    }
	    if (typeof values.excludes === 'string') {
		values.excludes = values.excludes
		    .split(/\r?\n/)
		    .map(value => value.trim())
		    .filter(value => value.length > 0);
	    }
	    return values;
	},

	column1: [
	    {
		xtype: 'displayfield',
		fieldLabel: gettext('Host'),
		name: 'host-display',
		cbind: { value: '{host}' },
	    },
	    {
		xtype: 'hiddenfield',
		name: 'host',
		cbind: { value: '{host}' },
	    },
	    {
		xtype: 'pbsDataStoreSelector',
		name: 'store',
		fieldLabel: gettext('Datastore'),
		allowBlank: false,
	    },
	    {
		xtype: 'pbsNamespaceSelector',
		name: 'ns',
		fieldLabel: gettext('Namespace'),
		forceSelection: false,
		cbind: { deleteEmpty: '{!isCreate}', value: '{namespace}' },
		emptyText: gettext('Root (shared datastore namespace)'),
	    },
	    {
		xtype: 'pbsCalendarEvent',
		name: 'schedule',
		fieldLabel: gettext('Schedule'),
		emptyText: gettext('none (manual only)'),
		cbind: { deleteEmpty: '{!isCreate}' },
	    },
	],

	column2: [
	    {
		xtype: 'proxmoxKVComboBox',
		name: 'snapshot-mode',
		fieldLabel: gettext('Snapshot Mode'),
		value: 'auto',
		comboItems: [
		    ['auto', gettext('Automatic')],
		    ['snapshot', gettext('Require Snapshot')],
		    ['direct', gettext('Direct')],
		],
	    },
	    {
		xtype: 'proxmoxKVComboBox',
		name: 'change-detection-mode',
		fieldLabel: gettext('Change Detection'),
		value: 'metadata',
		comboItems: [
		    ['metadata', gettext('Metadata')],
		    ['data', gettext('Data')],
		    ['legacy', gettext('Legacy')],
		],
	    },
	    {
		xtype: 'proxmoxcheckbox',
		name: 'enable',
		fieldLabel: gettext('Enabled'),
		uncheckedValue: false,
		value: true,
	    },
	],

	columnB: [
	    {
		xtype: 'displayfield',
		value: gettext('New jobs default to a device namespace. Missing namespaces are created if you have Datastore.Modify permission. Generated keys can use any backup ID in their authorized namespace; additional access can be granted in Permissions.'),
	    },
	    {
		xtype: 'textarea',
		name: 'sources',
		fieldLabel: gettext('Sources'),
		allowBlank: false,
		height: 100,
		emptyText: gettext('One filesystem, volume or path per line, for example C:\\ or /'),
		getSubmitValue: function() {
		    return this.getValue();
		},
	    },
	    {
		xtype: 'textarea',
		name: 'excludes',
		fieldLabel: gettext('Excludes'),
		height: 90,
		emptyText: gettext('One path per line, for example C:\\Temp or /var/cache'),
		getSubmitValue: function() {
		    return this.getValue();
		},
	    },
	    {
		xtype: 'proxmoxtextfield',
		name: 'comment',
		fieldLabel: gettext('Comment'),
		cbind: { deleteEmpty: '{!isCreate}' },
	    },
	],

	advancedColumn1: [
	    {
		xtype: 'pmxDisplayEditField',
		fieldLabel: gettext('Job ID'),
		emptyText: gettext('Autogenerate'),
		name: 'id',
		allowBlank: true,
		regex: PBS.Utils.SAFE_ID_RE,
		cbind: { editable: '{isCreate}' },
	    },
	],
    },

    setValues: function(values) {
	if (Ext.isArray(values.sources)) {
	    values.sources = values.sources.join('\n');
	}
	if (Ext.isArray(values.excludes)) {
	    values.excludes = values.excludes.join('\n');
	}
	this.callParent([values]);
    },
});
