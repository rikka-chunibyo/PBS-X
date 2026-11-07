package com.pbsx.android;

public final class SlowExportFixtureService extends ExportFixtureService {
  public SlowExportFixtureService(android.content.Context context) {
    super(context);
  }

  @Override
  protected int mode() {
    return 2;
  }
}
