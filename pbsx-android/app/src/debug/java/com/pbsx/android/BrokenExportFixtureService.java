package com.pbsx.android;

public final class BrokenExportFixtureService extends ExportFixtureService {
  public BrokenExportFixtureService(android.content.Context context) {
    super(context);
  }

  @Override
  protected int mode() {
    return 1;
  }
}
