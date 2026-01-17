# Weather MCP Server Extension
# Based on the standard MCP weather server example
# Uses the National Weather Service API (weather.gov)

HEADERS = {
    "User-Agent": "starlark-mcp-weather-extension",
    "Accept": "application/geo+json",
}

def get_forecast(params):
    """Get weather forecast for a location using latitude and longitude"""
    latitude = params.get("latitude", "")
    longitude = params.get("longitude", "")

    if not latitude:
        return error_response("latitude parameter is required")

    if not longitude:
        return error_response("longitude parameter is required")

    points_response = http.get(
        url = "https://api.weather.gov/points/{},{}".format(latitude, longitude),
        headers = HEADERS,
    )

    if points_response.get("status_code", 0) != 200:
        return error_response("Failed to get forecast grid: {}".format(
            points_response.get("body", "Unknown error"),
        ))

    points_data = parse_json_response(points_response)
    if not points_data:
        return error_response("Failed to parse forecast grid response")

    properties = points_data.get("properties", {})
    forecast_url = properties.get("forecast", "")

    if not forecast_url:
        return error_response("No forecast URL found in response")

    forecast_response = http.get(
        url = forecast_url,
        headers = HEADERS,
    )

    if forecast_response.get("status_code", 0) != 200:
        return error_response("Failed to get forecast: {}".format(
            forecast_response.get("body", "Unknown error"),
        ))

    forecast_data = parse_json_response(forecast_response)
    if not forecast_data:
        return error_response("Failed to parse forecast response")

    forecast_properties = forecast_data.get("properties", {})
    periods = forecast_properties.get("periods", [])

    if not periods:
        return {
            "content": [{"type": "text", "text": "No forecast data available"}],
        }

    # Build structured data
    forecast_periods = []
    for period in periods[:7]:
        forecast_periods.append({
            "name": period.get("name", "Unknown"),
            "temperature": period.get("temperature"),
            "temperatureUnit": period.get("temperatureUnit", "F"),
            "windSpeed": period.get("windSpeed", ""),
            "windDirection": period.get("windDirection", ""),
            "shortForecast": period.get("shortForecast", ""),
            "detailedForecast": period.get("detailedForecast", ""),
            "isDaytime": period.get("isDaytime", True),
        })

    structured = {
        "latitude": latitude,
        "longitude": longitude,
        "periods": forecast_periods,
    }

    # Build human-readable text
    output = "Weather Forecast for {}, {}\n".format(latitude, longitude)
    output += "=" * 50 + "\n\n"

    for period in periods[:7]:
        output += "{}\n".format(period.get("name", "Unknown"))
        output += "-" * 30 + "\n"
        output += "Temperature: {}°{}\n".format(
            period.get("temperature", "?"),
            period.get("temperatureUnit", "F"),
        )
        output += "Wind: {} {}\n".format(
            period.get("windSpeed", "Unknown"),
            period.get("windDirection", ""),
        )
        output += "Forecast: {}\n".format(period.get("shortForecast", "No forecast"))

        if period.get("detailedForecast"):
            output += "\n{}\n".format(period["detailedForecast"])

        output += "\n"

    return {
        "content": [{"type": "text", "text": output}],
        "structuredContent": structured,
    }

def get_alerts(params):
    """Get weather alerts for a US state using two-letter state code"""
    state = params.get("state", "")

    if not state:
        return error_response("state parameter is required")

    if len(state) != 2:
        return error_response("State code must be 2 letters (e.g., CA, NY, TX)")

    alerts_response = http.get(
        url = "https://api.weather.gov/alerts/active/area/{}".format(state.upper()),
        headers = HEADERS,
    )

    if alerts_response.get("status_code", 0) != 200:
        return error_response("Failed to get alerts: {}".format(
            alerts_response.get("body", "Unknown error"),
        ))

    alerts_data = parse_json_response(alerts_response)
    if not alerts_data:
        return error_response("Failed to parse alerts response")

    features = alerts_data.get("features", [])

    if not features:
        return {
            "content": [{"type": "text", "text": "No active weather alerts for {}".format(state.upper())}],
            "structuredContent": {"state": state.upper(), "alerts": []},
        }

    # Build structured data
    alerts_list = []
    for feature in features:
        properties = feature.get("properties", {})
        alerts_list.append({
            "event": properties.get("event", "Unknown Event"),
            "severity": properties.get("severity", "Unknown"),
            "urgency": properties.get("urgency", "Unknown"),
            "certainty": properties.get("certainty", "Unknown"),
            "areaDesc": properties.get("areaDesc", ""),
            "onset": properties.get("onset", ""),
            "expires": properties.get("expires", ""),
            "headline": properties.get("headline", ""),
            "description": properties.get("description", ""),
        })

    structured = {
        "state": state.upper(),
        "alertCount": len(features),
        "alerts": alerts_list,
    }

    # Build human-readable text
    output = "Active Weather Alerts for {}\n".format(state.upper())
    output += "=" * 50 + "\n\n"
    output += "Found {} active alert(s)\n\n".format(len(features))

    for feature in features:
        properties = feature.get("properties", {})

        output += "{}  {}\n".format(
            get_severity_emoji(properties.get("severity", "")),
            properties.get("event", "Unknown Event"),
        )
        output += "-" * 50 + "\n"

        output += "Severity: {}\n".format(properties.get("severity", "Unknown"))
        output += "Urgency: {}\n".format(properties.get("urgency", "Unknown"))
        output += "Certainty: {}\n".format(properties.get("certainty", "Unknown"))

        if properties.get("areaDesc"):
            output += "Area: {}\n".format(properties["areaDesc"])

        if properties.get("onset"):
            output += "Onset: {}\n".format(properties["onset"])

        if properties.get("expires"):
            output += "Expires: {}\n".format(properties["expires"])

        if properties.get("headline"):
            output += "\n{}\n".format(properties["headline"])

        if properties.get("description"):
            desc = properties["description"]
            if len(desc) > 500:  # Truncate long descriptions for readability
                desc = desc[:500] + "..."
            output += "\n{}\n".format(desc)

        output += "\n"

    return {
        "content": [{"type": "text", "text": output}],
        "structuredContent": structured,
    }

def get_current_conditions(params):
    """Get current weather conditions for a location"""
    latitude = params.get("latitude", "")
    longitude = params.get("longitude", "")

    if not latitude:
        return error_response("latitude parameter is required")

    if not longitude:
        return error_response("longitude parameter is required")

    points_response = http.get(
        url = "https://api.weather.gov/points/{},{}".format(latitude, longitude),
        headers = HEADERS,
    )

    if points_response.get("status_code", 0) != 200:
        return error_response("Failed to get weather station: {}".format(
            points_response.get("body", "Unknown error"),
        ))

    points_data = parse_json_response(points_response)
    if not points_data:
        return error_response("Failed to parse weather station response")

    properties = points_data.get("properties", {})
    stations_url = properties.get("observationStations", "")

    if not stations_url:
        return error_response("No observation stations URL found")

    stations_response = http.get(
        url = stations_url,
        headers = HEADERS,
    )

    if stations_response.get("status_code", 0) != 200:
        return error_response("Failed to get observation stations")

    stations_data = parse_json_response(stations_response)
    if not stations_data:
        return error_response("Failed to parse stations response")

    features = stations_data.get("features", [])
    if not features:
        return error_response("No observation stations found")

    station_id = features[0].get("properties", {}).get("stationIdentifier", "")
    if not station_id:
        return error_response("No station identifier found")

    observation_response = http.get(
        url = "https://api.weather.gov/stations/{}/observations/latest".format(station_id),
        headers = HEADERS,
    )

    if observation_response.get("status_code", 0) != 200:
        return error_response("Failed to get current conditions")

    observation_data = parse_json_response(observation_response)
    if not observation_data:
        return error_response("Failed to parse observation response")

    obs_properties = observation_data.get("properties", {})

    # Extract values for structured data
    temp = obs_properties.get("temperature", {})
    temp_c = temp.get("value") if temp else None
    temp_f = (temp_c * 9.0 / 5.0) + 32 if temp_c != None else None

    wind_speed = obs_properties.get("windSpeed", {})
    wind_dir = obs_properties.get("windDirection", {})
    humidity = obs_properties.get("relativeHumidity", {})
    pressure = obs_properties.get("barometricPressure", {})

    # Build structured data
    structured = {
        "latitude": latitude,
        "longitude": longitude,
        "station": station_id,
        "timestamp": obs_properties.get("timestamp", ""),
        "conditions": obs_properties.get("textDescription", ""),
        "temperature": {
            "celsius": temp_c,
            "fahrenheit": math.round(temp_f, 1) if temp_f != None else None,
        },
        "windSpeed": {
            "value": wind_speed.get("value") if wind_speed else None,
            "unit": "km/h",
        },
        "windDirection": {
            "degrees": wind_dir.get("value") if wind_dir else None,
        },
        "humidity": {
            "value": humidity.get("value") if humidity else None,
            "unit": "%",
        },
        "pressure": {
            "value": pressure.get("value") if pressure else None,
            "unit": "Pa",
        },
    }

    # Build human-readable text
    output = "Current Weather Conditions\n"
    output += "Location: {}, {}\n".format(latitude, longitude)
    output += "Station: {}\n".format(station_id)
    output += "=" * 50 + "\n\n"

    if temp_c != None:
        output += "Temperature: {}°F ({}°C)\n".format(math.round(temp_f, 1), math.round(temp_c, 1))

    if wind_speed and wind_speed.get("value") != None:
        output += "Wind: {} km/h".format(math.round(wind_speed["value"], 1))
        if wind_dir and wind_dir.get("value") != None:
            output += " from {}°".format(wind_dir["value"])
        output += "\n"

    if humidity and humidity.get("value") != None:
        output += "Humidity: {}%\n".format(int(humidity["value"]))

    if pressure and pressure.get("value") != None:
        output += "Pressure: {} Pa\n".format(int(pressure["value"]))

    if obs_properties.get("textDescription"):
        output += "\nConditions: {}\n".format(obs_properties["textDescription"])

    if obs_properties.get("timestamp"):
        output += "\nObserved at: {}\n".format(obs_properties["timestamp"])

    return {
        "content": [{"type": "text", "text": output}],
        "structuredContent": structured,
    }

# Helper functions
def parse_json_response(response):
    """Parse JSON from HTTP response"""
    if response.get("json"):
        return response["json"]
    try_parse = response.get("body", "{}")
    if try_parse.strip():
        return json.decode(try_parse)
    return None

def get_severity_emoji(severity):
    """Get an emoji based on alert severity"""
    severity_map = {
        "Extreme": "🔴",
        "Severe": "🟠",
        "Moderate": "🟡",
        "Minor": "🟢",
        "Unknown": "⚪",
    }
    return severity_map.get(severity, "⚪")

def error_response(message):
    """Create an error response"""
    return {
        "content": [{"type": "text", "text": "Error: " + message}],
        "isError": True,
    }

# Extension definition
def describe_extension():
    """Define the Weather MCP extension based on standard MCP server example"""
    return Extension(
        name = "weather",
        version = "1.0.0",
        description = "Weather information using National Weather Service API - standard MCP server example",
        tools = [
            Tool(
                name = "get_forecast",
                description = "Get weather forecast for a location using latitude and longitude coordinates",
                parameters = [
                    ToolParameter(
                        name = "latitude",
                        param_type = "string",
                        required = True,
                        description = "Latitude of the location (e.g., '38.8894')",
                    ),
                    ToolParameter(
                        name = "longitude",
                        param_type = "string",
                        required = True,
                        description = "Longitude of the location (e.g., '-77.0352')",
                    ),
                ],
                handler = get_forecast,
            ),
            Tool(
                name = "get_alerts",
                description = "Get active weather alerts for a US state using two-letter state code",
                parameters = [
                    ToolParameter(
                        name = "state",
                        param_type = "string",
                        required = True,
                        description = "Two-letter US state code (e.g., CA, NY, TX)",
                    ),
                ],
                handler = get_alerts,
            ),
            Tool(
                name = "get_current_conditions",
                description = "Get current weather conditions for a location using latitude and longitude coordinates",
                parameters = [
                    ToolParameter(
                        name = "latitude",
                        param_type = "string",
                        required = True,
                        description = "Latitude of the location (e.g., '38.8894')",
                    ),
                    ToolParameter(
                        name = "longitude",
                        param_type = "string",
                        required = True,
                        description = "Longitude of the location (e.g., '-77.0352')",
                    ),
                ],
                handler = get_current_conditions,
            ),
        ],
    )
