package com.pbsx.android;

import android.content.Context;
import android.content.SharedPreferences;
import java.util.HashMap;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.Set;
import org.json.JSONArray;

/** Preserve the emulator application's existing state around service/UI fixtures. */
final class TaskFixtureState implements AutoCloseable {
  private final Context context;
  private final JSONArray pending;
  private final Map<String, Map<String, ?>> preferences = new LinkedHashMap<>();

  TaskFixtureState(Context c) throws Exception {
    context = c;
    if (Operations.running()) throw new Exception("A transfer is already running");
    pending = PendingBackups.all(c);
    for (String name : new String[] {"jobs-v1", "settings", "vault"}) {
      SharedPreferences p = c.getSharedPreferences(name, 0);
      preferences.put(name, new HashMap<>(p.getAll()));
      if (!p.edit().clear().commit()) throw new Exception("Cannot prepare task fixture");
    }
    PendingBackups.install(c, new JSONArray());
  }

  @Override
  public void close() throws Exception {
    if (Operations.running()) throw new Exception("Cannot restore task fixture during a transfer");
    for (Map.Entry<String, Map<String, ?>> entry : preferences.entrySet()) {
      SharedPreferences.Editor edit =
          context.getSharedPreferences(entry.getKey(), 0).edit().clear();
      for (Map.Entry<String, ?> item : entry.getValue().entrySet()) {
        Object value = item.getValue();
        if (value instanceof String) edit.putString(item.getKey(), (String) value);
        else if (value instanceof Integer) edit.putInt(item.getKey(), (Integer) value);
        else if (value instanceof Long) edit.putLong(item.getKey(), (Long) value);
        else if (value instanceof Boolean) edit.putBoolean(item.getKey(), (Boolean) value);
        else if (value instanceof Float) edit.putFloat(item.getKey(), (Float) value);
        else if (value instanceof Set) edit.putStringSet(item.getKey(), (Set<String>) value);
        else throw new Exception("Unknown fixture preference type");
      }
      if (!edit.commit()) throw new Exception("Cannot restore task fixture");
    }
    PendingBackups.install(context, pending);
    ScheduleService.configureAll(context);
  }
}
