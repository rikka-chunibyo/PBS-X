Ext.define('PBS.HostPanel', {
    extend: 'Ext.tab.Panel',
    alias: 'widget.pbsHostPanel',

    stateId: 'pbsx-host-panel',
    stateful: true,
    stateEvents: ['tabchange'],
    border: false,

    items: [
	{ xtype: 'pbsHostSummary', itemId: 'summary' },
	{ xtype: 'pbsHostBackupSnapshotView', itemId: 'backups' },
	{ xtype: 'pbsHostInventoryView', itemId: 'inventory' },
	{ xtype: 'pbsHostBackupJobView', itemId: 'jobs' },
	{ xtype: 'pbsHostBackupRunView', itemId: 'runs' },
	{ xtype: 'pbsHostRestoreRunView', itemId: 'restores' },
	{ xtype: 'pbsHostAgentView', itemId: 'agent' },
    ],

    initComponent: function() {
	let me = this;
	me.title = `${gettext('Host')}: ${me.host}`;
	for (const item of me.items) {
	    item.host = me.host;
	}
	me.callParent();
    },
});
