package com.pbsx.android;

import android.app.Activity;
import android.content.Intent;
import android.os.Bundle;
import android.widget.LinearLayout;

/** Selects an existing host history without changing device/control identity. */
public final class SelectBackupActivity extends Activity {
  private BackupCatalogue catalogue;
  private LinearLayout page;

  @Override
  public void onCreate(Bundle saved) {
    super.onCreate(saved);
    LinearLayout root = Ui.screen(this, "Existing backup");
    page = Ui.scroll(root);
    catalogue =
        new BackupCatalogue(
            this,
            group -> {
              setResult(
                  RESULT_OK,
                  new Intent()
                      .putExtra("store", group.optString("store"))
                      .putExtra("namespace", group.optString("namespace"))
                      .putExtra("backup_id", group.optString("backup_id"))
                      .putExtra("connection_id", group.optString("connection_id")));
              finish();
            },
            this::finish,
            saved);
    if (saved == null) catalogue.connection(getIntent().getStringExtra("connection_id"));
    catalogue.chooseHistory();
  }

  @Override
  protected void onResume() {
    super.onResume();
    catalogue.setActive(true);
    catalogue.render(page);
  }

  @Override
  protected void onPause() {
    catalogue.setActive(false);
    super.onPause();
  }

  @Override
  protected void onDestroy() {
    catalogue.close();
    super.onDestroy();
  }

  @Override
  protected void onSaveInstanceState(Bundle saved) {
    catalogue.save(saved);
    super.onSaveInstanceState(saved);
  }
}
