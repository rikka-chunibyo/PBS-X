package com.pbsx.android;

import android.content.Context;
import android.database.Cursor;
import android.net.Uri;

/** Debug-only routing of the real shell provider reader to synthetic provider data. */
public class ExportFixtureService extends PrivilegedFiles {
  public ExportFixtureService(Context context) {
    super(context);
  }

  protected int mode() {
    return 0;
  }

  @Override
  protected DeviceExport.Source exportSource() throws Exception {
    int mode = mode();
    return new ShellProviderSource(0) {
      @Override
      protected Uri map(Uri uri) {
        return Uri.parse(
            "content://com.pbsx.android.export-fixture/" + uri.getAuthority() + uri.getPath());
      }

      @Override
      public Cursor query(Uri uri, String[] projection, String sort) throws Exception {
        if (mode == 1 && uri.getPath().equals("/raw_contacts"))
          throw new SecurityException("Synthetic provider denied read");
        if (mode == 2) Thread.sleep(30000);
        return super.query(uri, projection, sort);
      }
    };
  }
}
