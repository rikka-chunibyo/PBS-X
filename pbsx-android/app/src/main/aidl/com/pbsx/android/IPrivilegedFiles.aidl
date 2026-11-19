package com.pbsx.android;
import android.os.ParcelFileDescriptor;
interface IPrivilegedFiles {
    String stat(String path) = 0;
    String list(String path) = 1;
    ParcelFileDescriptor openRead(String path) = 2;
    String apps() = 3;
    ParcelFileDescriptor openExport(String category) = 4;
    String folders(String relativePath) = 5;
    void beginRestore(String relativeRoot, String freshFolder) = 6;
    String inspect(String path) = 7;
    void makeDirectory(String path) = 8;
    ParcelFileDescriptor openWrite(String path) = 9;
    void commitFile(String temporary, String path, boolean replace) = 10;
    void deleteStaged(String path) = 11;
    String restoreFolders(String relativePath) = 12;
    void destroy() = 16777114;
}
