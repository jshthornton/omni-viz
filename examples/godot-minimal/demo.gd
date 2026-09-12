extends Node3D
## Example shapes used by the gdviz README. Scenes pass their intent via
## user args (they arrive after "--" on the command line):
##   --drift=X  brightens every material — simulates global drift
##              (every pixel shifts a little: the changed-area budget's job)
##   --broken   hides the red box and throws the ball off its spot —
##              simulates a structural change (the per-pixel threshold's job)

func _ready() -> void:
	var drift := 0.0
	var broken := false
	for arg in OS.get_cmdline_user_args():
		if arg.begins_with("--drift="):
			drift = float(arg.trim_prefix("--drift="))
		elif arg == "--broken":
			broken = true
	if drift != 0.0:
		for mesh: MeshInstance3D in find_children("*", "MeshInstance3D", true, false):
			var mesh_res := mesh.mesh
			for i in mesh_res.get_surface_count():
				var mat := mesh_res.surface_get_material(i)
				if mat is StandardMaterial3D:
					mat.albedo_color = mat.albedo_color.lightened(drift)
	if broken:
		var box := get_node_or_null("Box")
		if box != null:
			box.visible = false
		var ball := get_node_or_null("Ball")
		if ball != null:
			ball.position += Vector3(0.9, 0.35, 0.4)
