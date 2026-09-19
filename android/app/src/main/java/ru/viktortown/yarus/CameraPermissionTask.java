package ru.viktortown.yarus;
import android.webkit.PermissionRequest;
public class CameraPermissionTask implements Runnable {
 public MainActivity activity; public PermissionRequest request;
 public CameraPermissionTask(MainActivity a, PermissionRequest r){activity=a;request=r;}
 @Override public void run(){activity.handleCameraRequest(request);}
}
