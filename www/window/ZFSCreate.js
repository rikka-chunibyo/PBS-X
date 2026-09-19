Ext.define('PBS.window.CreateZFS', {
    extend: 'Proxmox.window.Edit',
    xtype: 'pbsCreateZFS',

    subject: 'ZFS',

    showProgress: true,
    isCreate: true,

    onlineHelp: 'chapter_zfs',

    width: 800,

    url: '/nodes/localhost/disks/zfs',
    method: 'POST',
    items: [
        {
            xtype: 'inputpanel',
            onGetValues: function (values) {
                return values;
            },
            column1: [
                {
                    xtype: 'proxmoxtextfield',
                    name: 'name',
                    fieldLabel: gettext('Name'),
                    minLength: 3,
                    allowBlank: false,
                },
                {
                    xtype: 'proxmoxcheckbox',
                    name: 'add-datastore',
                    fieldLabel: gettext('Add as Datastore'),
                    value: '1',
                },
            ],
            column2: [
                {
                    xtype: 'proxmoxKVComboBox',
                    fieldLabel: gettext('RAID Level'),
                    name: 'raidlevel',
                    value: 'single',
                    listeners: {
                        change: function (field, value) {
                            let warning = field.up('inputpanel').down('#raid0Warning');
                            if (warning) {
                                warning.setHidden(value !== 'raid0');
                            }
                        },
                    },
                    comboItems: [
                        ['single', gettext('Single Disk')],
                        ['raid0', 'RAID0 (' + gettext('Stripe') + ')'],
                        ['mirror', 'Mirror'],
                        ['raid10', 'RAID10'],
                        ['raidz', 'RAIDZ'],
                        ['raidz2', 'RAIDZ2'],
                        ['raidz3', 'RAIDZ3'],
                    ],
                },
                {
                    xtype: 'proxmoxKVComboBox',
                    fieldLabel: gettext('Compression'),
                    name: 'compression',
                    value: 'on',
                    comboItems: [
                        ['on', 'on'],
                        ['off', 'off'],
                        ['gzip', 'gzip'],
                        ['lz4', 'lz4'],
                        ['lzjb', 'lzjb'],
                        ['zle', 'zle'],
                        ['zstd', 'zstd'],
                    ],
                },
                {
                    xtype: 'proxmoxintegerfield',
                    fieldLabel: gettext('ashift'),
                    minValue: 9,
                    maxValue: 16,
                    value: '12',
                    name: 'ashift',
                },
            ],
            columnB: [
                {
                    xtype: 'pmxMultiDiskSelector',
                    name: 'devices',
                    nodename: 'localhost',
                    typeParameter: 'usage-type',
                    valueField: 'name',
                    height: 200,
                    emptyText: gettext('No Disks unused'),
                },
                {
                    xtype: 'displayfield',
                    itemId: 'raid0Warning',
                    hidden: true,
                    userCls: 'pmx-hint',
                    value: gettext(
                        'RAID0 has no redundancy. Failure of any member disk can make the entire pool unavailable.',
                    ),
                },
            ],
        },
        {
            xtype: 'displayfield',
            padding: '5 0 0 0',
            userCls: 'pmx-hint',
            value:
                'Note: ZFS is not compatible with disks backed by a hardware ' +
                'RAID controller. For details see ' +
                '<a target="_blank" href="' +
                Proxmox.Utils.get_help_link('chapter_zfs') +
                '">the reference documentation</a>.',
        },
    ],
});
