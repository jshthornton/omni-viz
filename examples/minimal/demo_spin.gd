extends Node3D
## Rotates the rig every frame. Under gdviz the scene runs in movie mode
## (fixed fps), so the frame at quit_after is deterministic.

@export var spin_speed := 0.9


func _process(delta: float) -> void:
	$Rig.rotate_y(delta * spin_speed)
