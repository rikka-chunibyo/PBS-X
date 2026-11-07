package com.pbsx.android;

import android.content.AttributionSource;
import android.content.ContentResolver;
import android.content.res.AssetFileDescriptor;
import android.database.Cursor;
import android.net.Uri;
import android.os.Binder;
import android.os.Build;
import android.os.Bundle;
import android.os.IBinder;
import android.os.Process;
import java.io.InputStream;
import java.lang.reflect.InvocationTargetException;
import java.lang.reflect.Method;
import java.util.LinkedHashMap;
import java.util.Map;

/** The read-only subset of Android's content command, used only inside shell UserService. */
@android.annotation.TargetApi(31)
class ShellProviderSource implements DeviceExport.Source {
  private final int user;
  private final Object manager;
  private final Class<?> managerType, providerType;
  private final Map<String, Handle> handles = new LinkedHashMap<>();
  private final AttributionSource attribution;

  private static final class Handle {
    final IBinder token = new Binder();
    Object provider;
  }

  ShellProviderSource(int user) throws Exception {
    if (Build.VERSION.SDK_INT < 31)
      throw new Exception("Device data exports require Android 12 or later");
    this.user = user;
    manager =
        invoke(Class.forName("android.app.ActivityManager").getDeclaredMethod("getService"), null);
    managerType = Class.forName("android.app.IActivityManager");
    providerType = Class.forName("android.content.IContentProvider");
    attribution =
        new AttributionSource.Builder(Process.myUid())
            .setPackageName(Process.myUid() == 0 ? "root" : "com.android.shell")
            .build();
  }

  private static Object invoke(Method method, Object object, Object... args) throws Exception {
    try {
      method.setAccessible(true);
      return method.invoke(object, args);
    } catch (InvocationTargetException e) {
      Throwable cause = e.getCause();
      if (cause instanceof Exception) throw (Exception) cause;
      throw new Exception("Provider call failed", cause);
    }
  }

  protected Uri map(Uri uri) {
    return uri;
  }

  private Object provider(Uri uri) throws Exception {
    String authority = uri.getAuthority();
    Handle handle = handles.get(authority);
    if (handle != null) return handle.provider;
    handle = new Handle();
    Object holder =
        invoke(
            managerType.getMethod(
                "getContentProviderExternal", String.class, int.class, IBinder.class, String.class),
            manager,
            authority,
            user,
            handle.token,
            "PBS-X read-only export");
    if (holder == null) throw new Exception("Required Android data provider is unavailable");
    handle.provider = holder.getClass().getField("provider").get(holder);
    if (handle.provider == null)
      throw new Exception("Required Android data provider is unavailable");
    handles.put(authority, handle);
    return handle.provider;
  }

  public Cursor query(Uri original, String[] projection, String sort) throws Exception {
    Uri uri = map(original);
    Bundle args = new Bundle();
    if (sort != null) args.putString(ContentResolver.QUERY_ARG_SQL_SORT_ORDER, sort);
    return (Cursor)
        invoke(
            providerType.getMethod(
                "query",
                AttributionSource.class,
                Uri.class,
                String[].class,
                Bundle.class,
                Class.forName("android.os.ICancellationSignal")),
            provider(uri),
            attribution,
            uri,
            projection,
            args,
            null);
  }

  public InputStream open(Uri original) throws Exception {
    Uri uri = map(original);
    AssetFileDescriptor fd =
        (AssetFileDescriptor)
            invoke(
                providerType.getMethod(
                    "openAssetFile",
                    AttributionSource.class,
                    Uri.class,
                    String.class,
                    Class.forName("android.os.ICancellationSignal")),
                provider(uri),
                attribution,
                uri,
                "r",
                null);
    if (fd == null) throw new Exception("Required attachment is unavailable");
    try {
      return fd.createInputStream();
    } catch (Exception e) {
      fd.close();
      throw e;
    }
  }

  public void close() throws Exception {
    Exception error = null;
    for (Map.Entry<String, Handle> entry : handles.entrySet()) {
      try {
        invoke(
            managerType.getMethod(
                "removeContentProviderExternalAsUser", String.class, IBinder.class, int.class),
            manager,
            entry.getKey(),
            entry.getValue().token,
            user);
      } catch (Exception e) {
        error = e;
      }
    }
    handles.clear();
    if (error != null) throw error;
  }
}
