package com.pbsx.android;

/** Remove only this application's orphaned private export cache on process startup. */
public final class BackupApplication extends android.app.Application {
  @Override
  public void onCreate() {
    super.onCreate();
    try {
      Vault.data(this);
    } catch (Exception ignored) {
      /* Setup UI reports damaged or old test vaults. */
    }
    ExportCache.clearStale(this);
  }
}
