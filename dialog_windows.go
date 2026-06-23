//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

func selectDirDialog(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-STA", "-ExecutionPolicy", "Bypass", "-Command", windowsOpenFolderDialogScript())
	cmd.SysProcAttr = windowsHideConsole()
	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 2 {
			return "", errDirSelectionCanceled
		}
		if errors.As(err, &exitErr) {
			stderr := strings.TrimSpace(string(exitErr.Stderr))
			if stderr != "" {
				return "", fmt.Errorf("%w：%s", err, stderr)
			}
		}
		return "", err
	}

	path := strings.TrimSpace(string(output))
	if path == "" {
		return "", errDirSelectionCanceled
	}
	return path, nil
}

// windowsOpenFolderDialogScript 使用 Windows Common Item Dialog，界面样式与资源管理器打开文件夹窗口一致。
func windowsOpenFolderDialogScript() string {
	return `
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
Add-Type -TypeDefinition @"
using System;
using System.Runtime.InteropServices;

[Flags]
public enum FOS : uint
{
    FOS_NOCHANGEDIR = 0x00000008,
    FOS_PICKFOLDERS = 0x00000020,
    FOS_FORCEFILESYSTEM = 0x00000040,
    FOS_PATHMUSTEXIST = 0x00000800
}

public enum SIGDN : uint
{
    SIGDN_FILESYSPATH = 0x80058000
}

[ComImport]
[Guid("DC1C5A9C-E88A-4DDE-A5A1-60F82A20AEF7")]
public class FileOpenDialog
{
}

[ComImport]
[Guid("43826D1E-E718-42EE-BC55-A1E261C37BFE")]
[InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
public interface IShellItem
{
    void BindToHandler(IntPtr pbc, ref Guid bhid, ref Guid riid, out IntPtr ppv);
    void GetParent(out IShellItem ppsi);
    void GetDisplayName(SIGDN sigdnName, out IntPtr ppszName);
    void GetAttributes(uint sfgaoMask, out uint psfgaoAttribs);
    void Compare(IShellItem psi, uint hint, out int piOrder);
}

[ComImport]
[Guid("42f85136-db7e-439c-85f1-e4075d135fc8")]
[InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
public interface IFileDialog
{
    [PreserveSig]
    int Show(IntPtr hwndOwner);
    void SetFileTypes(uint cFileTypes, IntPtr rgFilterSpec);
    void SetFileTypeIndex(uint iFileType);
    void GetFileTypeIndex(out uint piFileType);
    void Advise(IntPtr pfde, out uint pdwCookie);
    void Unadvise(uint dwCookie);
    void SetOptions(FOS fos);
    void GetOptions(out FOS pfos);
    void SetDefaultFolder(IShellItem psi);
    void SetFolder(IShellItem psi);
    void GetFolder(out IShellItem ppsi);
    void GetCurrentSelection(out IShellItem ppsi);
    void SetFileName([MarshalAs(UnmanagedType.LPWStr)] string pszName);
    void GetFileName([MarshalAs(UnmanagedType.LPWStr)] out string pszName);
    void SetTitle([MarshalAs(UnmanagedType.LPWStr)] string pszTitle);
    void SetOkButtonLabel([MarshalAs(UnmanagedType.LPWStr)] string pszText);
    void SetFileNameLabel([MarshalAs(UnmanagedType.LPWStr)] string pszLabel);
    void GetResult(out IShellItem ppsi);
    void AddPlace(IShellItem psi, int fdap);
    void SetDefaultExtension([MarshalAs(UnmanagedType.LPWStr)] string pszDefaultExtension);
    void Close(int hr);
    void SetClientGuid(ref Guid guid);
    void ClearClientData();
    void SetFilter(IntPtr pFilter);
}

public static class NativeFolderPicker
{
    private const int HRESULT_CANCELLED = unchecked((int)0x800704C7);

    public static string PickFolder()
    {
        var dialog = (IFileDialog)new FileOpenDialog();
        dialog.SetOptions(FOS.FOS_PICKFOLDERS | FOS.FOS_FORCEFILESYSTEM | FOS.FOS_PATHMUSTEXIST | FOS.FOS_NOCHANGEDIR);
        dialog.SetTitle("Open Folder");
        dialog.SetOkButtonLabel("Select folder");
        dialog.SetFileNameLabel("文件夹:");

        int result = dialog.Show(IntPtr.Zero);
        if (result == HRESULT_CANCELLED)
        {
            return null;
        }
        if (result < 0)
        {
            Marshal.ThrowExceptionForHR(result);
        }

        IShellItem item;
        dialog.GetResult(out item);

        IntPtr pathPtr;
        item.GetDisplayName(SIGDN.SIGDN_FILESYSPATH, out pathPtr);
        try
        {
            return Marshal.PtrToStringUni(pathPtr);
        }
        finally
        {
            Marshal.FreeCoTaskMem(pathPtr);
            Marshal.ReleaseComObject(item);
            Marshal.ReleaseComObject(dialog);
        }
    }
}
"@

try {
  $path = [NativeFolderPicker]::PickFolder()
  if ([string]::IsNullOrWhiteSpace($path)) {
    exit 2
  }
  [Console]::Out.WriteLine($path)
  exit 0
} catch {
  [Console]::Error.WriteLine($_.Exception.Message)
  exit 1
}
`
}
