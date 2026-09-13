// VisualTestCapture — omniviz capture glue for Unity.
//
// Runs under the command driver:
//
//	unity-editor -batchmode -projectPath . -executeMethod VisualTestCapture.Capture \
//	    --scene=Lobby --seed=1234 -- <OMNIVIZ_OUTPUT path arrives via env>
//
// The method opens the requested scene, renders a fixed number of frames,
// saves a screenshot into $OMNIVIZ_OUTPUT and exits. omniviz auto-collects
// the PNG (the shot has no paths glob).
//
// Requirements:
//   - Do NOT pass -nographics: ScreenCapture needs a real render. CI runners
//     need a GPU (glcore/vulkan); game CI farms do this routinely.
//   - Pin the graphics API (-force-vulkan or -force-glcore) — switching APIs
//     between runs changes pixels everywhere.
using System;
using System.Environment;
using UnityEditor;
using UnityEditor.SceneManagement;
using UnityEngine;
using UnityEngine.SceneManagement;

public static class VisualTestCapture
{
    static int _frames;
    static int _quitAfter = 90;
    static string _outPath;

    public static void Capture()
    {
        var scene = GetArg("--scene", "Main");
        var seed = int.Parse(GetArg("--seed", "1234"));
        _quitAfter = int.Parse(GetArg("--quit-after", "90"));
        _outPath = System.IO.Path.Combine(
            Getenv("OMNIVIZ_OUTPUT") ?? ".",
            GetArg("--out", "shot") + ".png");

        // Determinism first: same seed, same fixed timestep, no vsync
        // wall-clock coupling, no dynamic resolution surprises.
        Random.InitState(seed);
        Time.fixedDeltaTime = 1f / 60f;
        QualitySettings.vSyncCount = 0;
        Application.targetFrameRate = 60;
        UnityEngine.XR.XRSettings.enabled = false;

        var open = EditorSceneManager.OpenScene($"Assets/Scenes/{scene}.unity", OpenSceneMode.Single);
        if (!open.IsValid())
        {
            Debug.LogError($"[omniviz] scene not found: Assets/Scenes/{scene}.unity");
            EditorApplication.Exit(2);
            return;
        }

        Debug.Log($"[omniviz] capturing {scene} -> {_outPath} after {_quitAfter} frames");
        EditorApplication.update += Tick;
    }

    static void Tick()
    {
        _frames++;
        if (_frames == _quitAfter)
        {
            // CaptureScreenshot is async — it lands at end of frame. Queue
            // the exit one frame later so the file is flushed.
            ScreenCapture.CaptureScreenshot(_outPath);
        }
        else if (_frames > _quitAfter && System.IO.File.Exists(_outPath))
        {
            EditorApplication.Exit(0);
        }
        else if (_frames > _quitAfter + 60)
        {
            Debug.LogError("[omniviz] screenshot never appeared");
            EditorApplication.Exit(3);
        }
    }

    static string GetArg(string name, string fallback)
    {
        // accept both "--scene Lobby" and "--scene=Lobby"
        var args = System.Environment.GetCommandLineArgs();
        for (int i = 0; i < args.Length; i++)
        {
            if (args[i] == name && i + 1 < args.Length) return args[i + 1];
            if (args[i].StartsWith(name + "=")) return args[i].Substring(name.Length + 1);
        }
        return fallback;
    }
}
