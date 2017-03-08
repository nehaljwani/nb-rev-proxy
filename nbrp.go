package main

import (
  "os"
  "fmt"
  "log"
  "time"
  "strconv"
  "net/http"
  "database/sql"

  "gopkg.in/redis.v5"
  "github.com/labstack/echo"
  /* Wanna use in-memory, local cache?
  "github.com/patrickmn/go-cache" */
  "github.com/parnurzeal/gorequest"
  "github.com/labstack/echo/middleware"
   _ "github.com/go-sql-driver/mysql"
)

func UpdateSlowQueries(query string, time_taken float64) {
    if (time_taken > resp_time_threshold.Seconds()) {
        /* Perform a no op if the query already exists */
        dbQuery := `
        INSERT INTO slow_queries(query, time_taken)
        VALUES(?, ?)
        ON DUPLICATE KEY UPDATE time_taken = time_taken`
        stmt, err := db.Prepare(dbQuery);
        if err != nil {
            log.Fatal(err)
        }
        _, err = stmt.Exec(query, time_taken)
        if err != nil {
            log.Fatal(err)
        }
        /* We may have hit the URL before and we wish to update it
        only if the current time taken is more */
        dbQuery = `
        UPDATE slow_queries
        SET time_taken = (?)
        WHERE query = (?) AND time_taken < (?)`
        stmt, err = db.Prepare(dbQuery);
        if err != nil {
            log.Fatal(err)
        }
        _, err = stmt.Exec(time_taken, query, time_taken)
        if err != nil {
            log.Fatal(err)
        }
    }
}

func UpdateQueryCount(query string) {
    dbQuery := `
    INSERT INTO queries(query, hitcount)
    VALUES(?, 1)
    ON DUPLICATE KEY UPDATE hitcount = hitcount + 1`
    stmt, err := db.Prepare(dbQuery);
    if err != nil {
        log.Fatal(err)
    }
    _, err = stmt.Exec(query)
    if err != nil {
        log.Fatal(err)
    }
}

func GetQueryStats() map[string]map[string]string {
    var (
        hitcount int
        time_taken float64
        query string
    )
    res := make(map[string]map[string]string)

    rows, err := db.Query("SELECT query, hitcount FROM queries")
    if err != nil {
        log.Fatal(err)
    }
    defer rows.Close()
    res["queries"] = make(map[string]string)
    for rows.Next() {
        err := rows.Scan(&query, &hitcount)
        if err != nil {
            log.Fatal(err)
        }
        res["queries"][query] = strconv.Itoa(hitcount)
    }

    dbQuery := `
    SELECT query, time_Taken
    FROM slow_queries
    WHERE time_taken > ?`
    stmt, err := db.Prepare(dbQuery);
    if err != nil {
        log.Fatal(err)
    }
    defer stmt.Close()
    rows, err = stmt.Query(resp_time_threshold.Seconds())
    if err != nil {
        log.Fatal(err)
    }
    defer rows.Close()
    res["slow_queries"] = make(map[string]string)
    for rows.Next() {
        err := rows.Scan(&query, &time_taken)
        if err != nil {
            log.Fatal(err)
        }
        res["slow_queries"][query] = fmt.Sprintf("%.2fs", time_taken)
    }

    return res
}

func UpdateStats(next echo.HandlerFunc) echo.HandlerFunc {
    return func(c echo.Context) error {
        start := time.Now()
        if err := next(c); err != nil {
            c.Error(err)
        }
        stop := time.Now()
        query := c.Request().RequestURI
        if c.Response().Status == 200 {
            // Update DB here (non-blocking)
            go UpdateSlowQueries(query, stop.Sub(start).Seconds())
            go UpdateQueryCount(query)
        }
        return nil
    }
}

func GetEnv(key, fallback string) string {
    value := os.Getenv(key)
    if len(value) == 0 {
        return fallback
    }
    return value
}

var (
    db *sql.DB
    cache_exp time.Duration
    resp_time_threshold time.Duration
)

func main() {
  var err error

  // Echo instance
  e := echo.New()

  // Middleware
  e.Use(middleware.Logger())
  e.Use(middleware.Recover())

  /* Make sure connection to DB is possible */
  dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s", GetEnv("DB_USER", "root"),
                     GetEnv("DB_PASSWORD", ""), GetEnv("DB_HOST", "localhost"),
                     GetEnv("DB_PORT", "3306"), GetEnv("DB_NAME", "nbrp"));

  db, err = sql.Open("mysql", dsn)
  if err != nil {
      e.Logger.Fatal(err)
  }
  err = db.Ping(); if err != nil {
      e.Logger.Fatal(err)
  }
  defer db.Close()

  cache_exp, err = time.ParseDuration(GetEnv("CACHE_EXPIRATION", "300s"))
  if err != nil {
      e.Logger.Fatal(err)
  }

  resp_time_threshold, err = time.ParseDuration(GetEnv("RESPONSE_TIME_THRESHOLD", "1s"))
  if err != nil {
      e.Logger.Fatal(err)
  }

  /* Local cache
  // Create a cache with a default expiration time of 5 minutes, and which
  // purges expired items every 30 seconds
  cc := cache.New(5*time.Minute, 30*time.Second) */

  client := redis.NewClient(&redis.Options{
      Addr:     fmt.Sprintf("%s:%s", GetEnv("REDIS_HOST", "localhost"),
                                     GetEnv("REDIS_PORT", "6379")),
      Password: GetEnv("REDIS_PASSWORD", ""),
      DB:       0,  // use default DB
  })
  _, err = client.Ping().Result()
  if err != nil {
      e.Logger.Fatal(err)
  }

  // This is a versioned API (idiomatic way?)
  v1 := e.Group("/api/v1")

  // Group specific middleware
  v1.Use(UpdateStats);

  v1.GET("/stats", func(c echo.Context) error {
      return c.JSONPretty(http.StatusOK, GetQueryStats(), " ")
  })

  // Route => handler
  v1.GET("*", func(c echo.Context) error {
    query := c.Param("*") + "?" + c.QueryString()

    /* Local cache
    cresp, found := cc.Get(query)
    if found {
        return c.String(http.StatusOK, cresp.(string))
    } */

    cresp, err := client.Get(query).Result()
    if err != nil {
        e.Logger.Warn(err)
    } else {
        return c.String(http.StatusOK, cresp)
    }

    /* Query is not present in cache */
    remoteURI := "http://webservices.nextbus.com/service/publicXMLFeed" + query
    request := gorequest.New()
    resp, body, errs := request.Get(remoteURI).End()
    if (errs != nil) {
        e.Logger.Warn(errs)
        return c.String(http.StatusInternalServerError, "Unable to proxy this request :'(")
    } else {
        // Set the value of the key `query` to `cresp`,
        // with the default expiration time

        /* Update cache (concurrently)
        cc.Set(query, body, cache.DefaultExpiration)*/
        go func() {
            err := client.Set(query, body, cache_exp).Err()
            if err != nil {
                e.Logger.Warn(err)
            }
        }()
        return c.String(resp.StatusCode, body)
    }
  })

  // Start server
  e.Logger.Fatal(e.Start(fmt.Sprintf(":%s", GetEnv("APP_PORT", "8080"))))
}
