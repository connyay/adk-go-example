def get_cat_fact(params):
    """Returns a random cat fact."""
    facts = [
        "Cats sleep 12-16 hours a day.",
        "A group of cats is called a 'clowder'.",
        "Cats have over 30 muscles in each ear.",
        "The first cat in space was French and named Felicette.",
        "Cats can rotate their ears 180 degrees.",
        "A cat's purr vibrates at a frequency of 25-150 Hz.",
        "Cats have a third eyelid called a haw.",
        "A cat's nose print is unique, like a human fingerprint.",
        "Cats can jump up to six times their length.",
        "Cats have whiskers on the backs of their front legs.",
    ]

    # Simple pseudo-random selection based on timestamp
    index = time.now() % len(facts)

    return {
        "content": [{"type": "text", "text": facts[index]}],
    }

def describe_extension():
    """Describes the extension and its tools."""
    return Extension(
        name = "cat_facts",
        version = "1.0.0",
        description = "Fun facts about cats",
        tools = [
            Tool(
                name = "get_cat_fact",
                description = "Get a random fact about cats",
                handler = get_cat_fact,
            ),
        ],
    )
