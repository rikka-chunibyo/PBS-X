Ext.define('PBS.window.HostEdit', {
    extend: 'Proxmox.window.Edit',
    alias: 'widget.pbsHostEdit',
    mixins: ['Proxmox.Mixin.CBind'],

    subject: gettext('Host'),
    fieldDefaults: { labelWidth: 120 },

    cbindData: function(initialConfig) {
	let me = this;
	let baseurl = '/api2/extjs/config/host';
	let name = initialConfig.name;

	me.isCreate = !name;
	me.url = name ? `${baseurl}/${encodeURIComponent(name)}` : baseurl;
	me.method = name ? 'PUT' : 'POST';
	me.autoLoad = !!name;

	return {};
    },

    items: {
	xtype: 'inputpanel',
	column1: [
	    {
		xtype: 'pmxDisplayEditField',
		name: 'name',
		fieldLabel: gettext('Host ID'),
		renderer: Ext.htmlEncode,
		allowBlank: false,
		minLength: 2,
		cbind: {
		    editable: '{isCreate}',
		},
	    },
	    {
		xtype: 'proxmoxcheckbox',
		name: 'enable',
		fieldLabel: gettext('Enabled'),
		checked: true,
		uncheckedValue: 0,
	    },
	],
	column2: [
	    {
		xtype: 'proxmoxtextfield',
		name: 'comment',
		fieldLabel: gettext('Comment'),
		cbind: {
		    deleteEmpty: '{!isCreate}',
		},
	    },
	],
    },
});
