package com.pbsx.android;
import android.os.ParcelFileDescriptor;
interface IPrivilegedFiles {
    String stat(String path) = 0;
    String list(String path) = 1;
    ParcelFileDescriptor openRead(String path) = 2;
    String apps() = 3;
    ParcelFileDescriptor openExport(String category) = 4;
    String folders(String relativePath) = 5;
    void destroy() = 16777114;
}
