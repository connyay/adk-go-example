def get_location(params):
    """Get the current location based on IP address"""
    response = http.get(
        url = "https://geoippls.com/v1.json",
        headers = {"Accept": "application/json"},
    )

    if response.get("status_code", 0) != 200:
        return {
            "content": [{"type": "text", "text": "Error: Failed to get location: " + response.get("body", "Unknown error")}],
            "isError": True,
        }

    data = response.get("json", {})
    if not data:
        body = response.get("body", "{}")
        data = json.decode(body) if body else {}

    coords = data.get("coordinates", {})

    output = {
        "city": data.get("city", "Unknown"),
        "region": data.get("region", "Unknown"),
        "region_code": data.get("region_code", ""),
        "country": data.get("country", "Unknown"),
        "postal_code": data.get("postal_code", ""),
        "timezone": data.get("timezone", ""),
        "latitude": str(coords.get("latitude", "")),
        "longitude": str(coords.get("longitude", "")),
    }

    location_str = "{}, {} {}".format(
        output["city"],
        output["region"],
        output["country"],
    )

    text = "Current Location: {}\n".format(location_str)
    text += "Coordinates: {}, {}\n".format(output["latitude"], output["longitude"])
    text += "Timezone: {}\n".format(output["timezone"])

    return {
        "content": [{"type": "text", "text": text}],
        "structuredContent": output,
    }

def describe_extension():
    """Describes the GeoIP extension"""
    return Extension(
        name = "geoip",
        version = "1.0.0",
        description = "Get current location based on IP address",
        tools = [
            Tool(
                name = "get_location",
                description = "Get the current geographic location based on IP address. Returns city, region, country, coordinates, and timezone.",
                handler = get_location,
            ),
        ],
    )
