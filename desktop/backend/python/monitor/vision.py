# THIS IS STILL JUST A DEMO
"""
Comments at your convience. Feel free to tinker.

Scrappy Vision code suggested by Ubuntufanboy.

Uses YOLOE for querying an image. L*tency is ~70ms per frame on a 5060ti
"""
from ultralytics import YOLO

def detect_with_direct_prompts(image_path, prompts, output_txt="detected_objects.txt"):
    """
    Uses YOLOE/YOLO-World with Direct Prompting to detect specific objects.
    
    Args:
        image_path (str): Path to the input image.
        prompts (list): A list of text strings (e.g., ["red car", "traffic light"]).
        output_txt (str): File to save the detected names.
    """
    # Load the model. 
    # Note: We use the standard version (e.g., 'yoloe-v8s.pt' or 'yolov8s-world.pt')
    # rather than the specialized '-pf' (prompt-free) version if available.
    # The standard weights are often better optimized for dynamic prompting.
    model = YOLO("yoloe-v8s-seg.pt")  

    # --- CRITICAL STEP: DIRECT PROMPTING ---
    # This restricts the model's vocabulary to ONLY what you specify.
    # It acts as a filter, ignoring everything else and boosting accuracy for these items.
    model.set_classes(prompts)

    # Run inference
    # We can often lower the confidence threshold slightly because 
    # we are now confident in *what* we are looking for.
    results = model.predict(source=image_path, conf=0.15, save=False)

    detected_names = set()

    # Process results
    for result in results:
        for box in result.boxes:
            # box.cls is the index in your *custom* list (prompts), not the global list
            class_id = int(box.cls[0])
            # Retrieve the name from the model's active names
            class_name = model.names[class_id]
            detected_names.add(class_name)

    # Save to text file
    try:
        with open(output_txt, "w") as f:
            if detected_names:
                # Write names sorted alphabetically
                f.write("\n".join(sorted(detected_names)))
                print(f"Success! Detected: {', '.join(sorted(detected_names))}")
            else:
                f.write("No objects detected from the prompt list.")
                print("No objects matched your prompts.")
                
    except IOError as e:
        print(f"Error writing to file: {e}")

# --- Usage Example ---
if __name__ == "__main__":
    # Define EXACTLY what you want to find. 
    # Be descriptive! "rusty metal container" works better than just "container".
    print("WARNING: Running directly. This should be imported into another script!")
    my_custom_prompts = [
        "Someprompt", 
        "Neuro can actually query multuiple things at a time for speed improvements", 
        "Another prompt. Ex. URL search bar"
    ]

    # Replace this filename with the NAME OF THE SCREENSHOT.
    detect_with_direct_prompts("screenshot.jpg", my_custom_prompts)
