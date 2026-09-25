# URL shortener

## Functional requirements
- POST to make a new short URL
- GET to convert the short URL in't it's original long URL

## Non functional requriementsd
- 100m new URLs created a day
- 1b redirects a day
- Service will run for 10 years

## Napkin maths
- 10 years * 365 days * 100m new short urls = 365b short urlss
- 62 ^ 6 = 56b is not enough
- 62 ^ 7 = 3.5T = we need a 7 digit short code


## Decisions and tradeoffs
