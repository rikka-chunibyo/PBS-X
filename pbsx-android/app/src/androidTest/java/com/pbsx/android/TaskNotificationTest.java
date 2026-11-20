package com.pbsx.android;

import static org.junit.Assert.*;

import android.Manifest;
import android.app.Instrumentation;
import android.app.NotificationManager;
import android.content.Context;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.os.Build;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import org.json.JSONObject;
import org.junit.Test;
import org.junit.runner.RunWith;

@RunWith(AndroidJUnit4.class)
public class TaskNotificationTest {
  @Test
  public void pausingBeforeSourcePreparationRemovesTheForegroundNotification() throws Exception {
    Instrumentation instrumentation = InstrumentationRegistry.getInstrumentation();
    Context c = instrumentation.getTargetContext();
    boolean granted =
        Build.VERSION.SDK_INT < 33
            || c.checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS)
                == PackageManager.PERMISSION_GRANTED;
    if (!granted)
      instrumentation
          .getUiAutomation()
          .grantRuntimePermission(c.getPackageName(), Manifest.permission.POST_NOTIFICATIONS);
    try (TaskFixtureState state = new TaskFixtureState(c)) {
      JSONObject task = new PendingBackupsTest().run(c);
      PendingBackups.put(c, task);
      BackupJob job = BackupJob.from(task.getJSONObject("job"));
      // Hold preparation until both service commands have run on the main
      // thread. This exercises Pause even before the Go client is attached.
      instrumentation
          .getUiAutomation()
          .adoptShellPermissionIdentity(
              "android.permission.START_FOREGROUND_SERVICES_FROM_BACKGROUND");
      try {
        synchronized (Vault.class) {
          c.startForegroundService(
              new Intent(c, BackupService.class).putExtra("job_config", job.json().toString()));
          c.startService(new Intent(c, BackupService.class).setAction("pause"));
          instrumentation.waitForIdleSync();
          long started = System.currentTimeMillis() + 5000;
          while (!Operations.running() && System.currentTimeMillis() < started) Thread.sleep(20);
          assertTrue("Transfer worker did not start", Operations.running());
          boolean posted = false;
          for (android.service.notification.StatusBarNotification n :
              c.getSystemService(NotificationManager.class).getActiveNotifications())
            if (n.getId() == 71) posted = true;
          assertTrue("Foreground notification was never posted", posted);
        }
      } finally {
        instrumentation.getUiAutomation().dropShellPermissionIdentity();
      }
      long end = System.currentTimeMillis() + 10000;
      boolean paused = false, notification = true;
      do {
        JSONObject pending = PendingBackups.get(c, job.id);
        paused =
            pending != null
                && "paused".equals(pending.optString("state"))
                && !Operations.running()
                && !BackupService.busy();
        notification = false;
        for (android.service.notification.StatusBarNotification n :
            c.getSystemService(NotificationManager.class).getActiveNotifications())
          if (n.getId() == 71) notification = true;
        if (!paused || notification) Thread.sleep(50);
      } while ((!paused || notification) && System.currentTimeMillis() < end);
      assertTrue("Backup did not retain its paused task", paused);
      assertFalse("Paused backup left a completion/progress notification", notification);
      assertEquals(task.getString("history"), PendingBackups.get(c, job.id).getString("history"));
      assertEquals(1, JobStore.history(c).length());
    } finally {
      c.stopService(new Intent(c, BackupService.class));
      // Revoking this permission can kill the instrumentation process. This
      // fixture runs on a disposable test emulator; leave the grant in place.
    }
  }
}
