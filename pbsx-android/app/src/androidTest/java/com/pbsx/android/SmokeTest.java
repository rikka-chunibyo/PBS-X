package com.pbsx.android;

import static org.junit.Assert.*;

import android.app.Instrumentation;
import android.content.Context;
import android.content.Intent;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import com.pbsx.core.mobilecore.Mobilecore;
import org.json.JSONObject;
import org.junit.Assume;
import org.junit.Test;
import org.junit.runner.RunWith;

@RunWith(AndroidJUnit4.class)
public class SmokeTest {
  @Test
  public void bindsNativeGoSocketsToWifi() throws Exception {
    String config = InstrumentationRegistry.getArguments().getString("pbsx_mock_config");
    Assume.assumeTrue(
        "Requires the loopback HTTPS network fixture", config != null && !config.isEmpty());
    Context c = InstrumentationRegistry.getInstrumentation().getTargetContext();
    boolean old = Settings.prefs(c).getBoolean("wifi", true);
    try {
      Settings.prefs(c).edit().putBoolean("wifi", true).commit();
      String result =
          Operations.client(c, config, new Operations.Cancellation())
              .listSnapshots("backup", "", "android-test");
      assertEquals(1, new JSONObject(result).getJSONArray("data").length());
    } finally {
      Settings.prefs(c).edit().putBoolean("wifi", old).commit();
    }
  }

  @Test
  public void keystoreEncryptsAndDecrypts() throws Exception {
    Context c = InstrumentationRegistry.getInstrumentation().getTargetContext();
    String old = Vault.load(c);
    String marker = "instrumentation-test-secret";
    try {
      Vault.save(c, new JSONObject().put("secret", marker).toString());
      assertTrue(Vault.load(c).contains(marker));
      assertFalse(c.getSharedPreferences("vault", 0).getString("connection", "").contains(marker));
    } finally {
      if (old.isEmpty()) c.getSharedPreferences("vault", 0).edit().remove("connection").commit();
      else Vault.save(c, old);
    }
  }

  @Test
  public void nativeGoEngineLoadsAndRejectsHttp() throws Exception {
    String cfg =
        new JSONObject()
            .put("server", "http://localhost:8007")
            .put("host_id", "android-test")
            .put("fingerprint", new String(new char[64]).replace('\0', '0'))
            .put("auth_id", "a@pbs!test")
            .put("secret", "test")
            .toString();
    try {
      Mobilecore.newClient(cfg);
      fail("HTTP was accepted");
    } catch (Exception expected) {
      assertTrue(expected.getMessage().contains("HTTPS"));
    }
  }

  @Test
  public void mainActivityLaunches() {
    Instrumentation i = InstrumentationRegistry.getInstrumentation();
    MainActivity activity =
        (MainActivity)
            i.startActivitySync(
                new Intent(i.getTargetContext(), MainActivity.class)
                    .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK));
    i.runOnMainSync(
        () -> {
          assertNotNull(activity.findViewById(android.R.id.content));
          activity.finish();
        });
  }
}
