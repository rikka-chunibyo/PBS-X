package com.pbsx.android;

import android.content.Context;
import android.content.SharedPreferences;
import org.json.JSONArray;

final class Settings {
  static SharedPreferences prefs(Context c) {
    return c.getSharedPreferences("settings", Context.MODE_PRIVATE);
  }

  static JSONArray sources(Context c) throws Exception {
    return new JSONArray(prefs(c).getString("sources", "[]"));
  }

  static String store(Context c) {
    return prefs(c).getString("store", "backup");
  }

  static String namespace(Context c) {
    return prefs(c).getString("namespace", "");
  }

  static void status(Context c, String message) {
    prefs(c)
        .edit()
        .putString("status", message)
        .putLong("status_time", System.currentTimeMillis())
        .apply();
  }
}
