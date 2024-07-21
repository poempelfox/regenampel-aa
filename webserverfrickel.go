
// Web serving functions (those that are called by the webserver)
//package radeaa
package main

import (
  //"encoding/json"
  "fmt"
  //"html/template"
  //"io/ioutil"
  "encoding/json"
  "log"
  "net/http"
  "net/url"
  "sort"
  "strconv"
  //"strings"
  "time"
)

// How old in seconds are values permitted to be to be considered "current"?
const maxcache = 60

// Regenampel "threshold"
const threshold = 0.1

// rateAttr returns the category number for the given precipitation rate (mm/h).
// FIXME? this function returns a float in the Javascript.
func rateCategory(pr float64) int {
  var rates = [...]float64 { 0.0, 0.1, 0.5, 4.0, 10.0, 73.0, 50000.0 }
  for i := len(rates) - 2; i >= 1; i-- {
    if (pr >= rates[i]) {
      return i
    }
  }
  return 0;
}

// rateAttr returns an attribute for the given precipitation rate (mm/h).
func rateAttr(pr float64) string {
  var categstr = [...]string { "kaum", "leichter", "mäßiger", "starker", "sehr starker", "Gewitter" }
  if (pr == 0.0) {
    return "kein"
  }
  return categstr[rateCategory(pr)];
}

// wayAttr returns an attribute for the given way percentage.
func wayAttr(per float64) string {
  if (per <= 0.4) {
    return "";
  }
  if (per <= 0.7) {
    return "ab der Hälfte des Weges";
  }
  return "gegen Ende des Weges";
}

// returns human readable time in n minutes.
// example: if you call timeIn(10) at 10:15, you would get "10:25".
func timeIn(minutes int) string {
  dt := time.Now()
  dt = dt.Add(time.Minute * time.Duration(minutes))
  return dt.Format("15:04")
}

func toMax(a [][]float64) []float64 {
  res := make([]float64, len(a))
  for i := 0; i < len(a); i++ {
    var curmax float64 = 0.0
    for _, v := range a[i] {
      if (v > curmax) {
        curmax = v
      }
    }
    res[i] = curmax
  }
  return res
}

func toMedian(a [][]float64) []float64 {
  res := make([]float64, len(a))
  for i := 0; i < len(a); i++ {
    cpy := make([]float64, len(a[i]))
    // FIXME? the javascript checks if the values in the array are >= 0 and only copies those.
    copy(cpy, a[i])
    sort.Float64s(cpy)
    l := len(cpy)
    if (l == 0) {
      res[i] = 0  // FIXME? the javascript aborts in this case and returns a non-array?!
    } else if ((l % 2) == 0) { // average the two elements that share the middle
      res[i] = (cpy[(l / 2) - 1] + cpy[l / 2]) / 2
    } else { // just one element in the middle, select that.
      res[i] = cpy[l / 2]
    }
  }
  return res
}

func testConditionForAllElements(a []float64, f func(float64) bool) bool {
  for _, e := range a {
    if (f(e)) {
      return true
    }
  }
  return false
}

func getIndexForCondition(a []float64, f func(float64) bool) int {
  for idx, e := range a {
    if (f(e)) {
      return idx
    }
  }
  return -1
}

// There would be a builtin for that in Go >= 1.21.
func getMaxForF64Slice(a []float64) float64 {
  var curmax float64 = 0
  for _, e := range a {
    if (e > curmax) {
      curmax = e
    }
  }
  return curmax
}

type regenampel_apiresponse struct {
  LocationName string     // Starting point, textual, e.g. "SomeStreet, Berlin [12345]"
  DestinationName string  // End point
  CaptureEpoch int64
  CenterYX [2]float64     // Coordinates [y,x] of ??? perhaps middle between start and end?
  TravelDuration float64
  LaceDuration float64
  TravelDistance float64
  TravelSpeed float64
  TimeStep int            // always? 1 (minutes?)
  CheckStep int           // always? 5 (minutes?)
  ForecastHorizon int     // always 120 (minutes)
  RainTable [][]float64   // Array of 24 float? arrays. 24 is 120/5, ForecastHorizon / CheckStep
                          // The inner arrays do not all have the exact same
                          // length, and the max length depends on the way
                          // length, it's (either LaceDuration or TravelDuration) / TimeStep
  LastUpdate string       // e.g. "Sat Jul 20 11:03:47 CEST 2024"
  LightColor int          // 0 = invalid, 1 = red, 2 = yellow, 3 = green
  Message1 string         // first message line
  Message2 string         // second message line
}

func Webfrickel_radeaa(w http.ResponseWriter, r *http.Request) {
  w.Header().Set("Access-Control-Allow-Origin", "*") // this is a public api, everyone may use it
  w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS") // any other voodoo makes no sense
  if (r.Method == "OPTIONS") { // nothing more to do than send the headers for the CORS crap.
    return
  }
  params := r.URL.Query()
  // https://regenampel.de/api/data?location=fauiwg%2C+Erlangen+%5B91052%5D&destination=&speed=0
  // https://regenampel.de/api/data?location=FSI-Zimmer+(FsIzWdvS)%2C+Erlangen+%5B91058%5D&destination=ZAM%2C+Erlangen+%5B91054%5D&speed=3
  var location string = params.Get("location")
  var destination string = params.Get("destination")
  var speedstr string = params.Get("speed")
  if ((len(location) < 3) || (len(speedstr) < 1)) {
    // At least startpoint and speed need to be given. If they're not, be
    // helpful and show a human readable page that explains what this is.
    http.ServeFile(w, r, "static/index.html")
    return
  }
  var dataformat string = params.Get("dataformat")
  if ((dataformat != "json") && (dataformat != "txt")) {
    dataformat = "json"
  }
  var prewarntimestr string = params.Get("prewarntime")
  prewarntime, err := strconv.ParseInt(prewarntimestr, 10, 0)
  if ((err != nil) || (prewarntime <= 0) || (prewarntime >= 120)) {
    prewarntime = 10 // Use the upstream default
  }
  log.Printf("start %s destination %s speedstr %s", location, destination, speedstr);
  // Now fetch upstream data
  urlq := url.Values{}
  urlq.Add("location", location)
  urlq.Add("destination", destination)
  urlq.Add("speed", speedstr)
  var url string = "https://regenampel.de/api/data?" + urlq.Encode()
  var httpc = &http.Client{ Timeout: (10 * time.Second) }
  inp, err := httpc.Get(url)
  if (err != nil) {
    log.Printf("[W] Upstream-Fetch failed for %s: %s", url, err.Error());
    http.Error(w, "ERROR: failed to fetch data from regenampel.de", 502)
    return
  }
  defer inp.Body.Close()
  var raar regenampel_apiresponse
  err = json.NewDecoder(inp.Body).Decode(&raar)
  if (err != nil) {
    log.Printf("[W] Invalid data from upstream for %s: %s", url, err.Error());
    http.Error(w, "ERROR: invalid JSON data received from regenampel.de", 502)
    return
  }
  log.Printf("got something. CaptureEpoch %d RainTable00 %f LastUpdate %s", raar.CaptureEpoch, raar.RainTable[0][0], raar.LastUpdate);
  // Now enrich the data...
  // This is reverse-engineered from the javascript in the webpage.
  // It's likely wrong, and a mess.
  graph_check_max_data := toMax(raar.RainTable)
  graph_check_avg_data := toMedian(raar.RainTable)
  raar.LightColor = 3 // default to green
  raar.Message1 = "kein Niederschlag"
  raar.Message2 = fmt.Sprintf("Vorhersage für die nächsten %d Stunden", raar.ForecastHorizon / 60.0)
  hf_xabovethres := func(x float64) bool { return (x >= threshold) }
  if (testConditionForAllElements(graph_check_max_data, hf_xabovethres)) {
    phase_begin_ind := getIndexForCondition(graph_check_max_data, hf_xabovethres)
    fmt.Printf("phase_begin_ind = %d\n", phase_begin_ind)
    phase_pause_ind := getIndexForCondition(graph_check_max_data[phase_begin_ind:],
                                            func(x float64) bool { return (x < threshold) })
    if (phase_pause_ind >= 0) { // We need to _absolute_ index, so we need to
      phase_pause_ind = phase_pause_ind + phase_begin_ind // add the starting offset
    }
    fmt.Printf("phase_pause_ind = %d\n", phase_pause_ind)
    endidx := phase_pause_ind
    if ((endidx < 0) || (endidx < phase_begin_ind)) {
      endidx = len(graph_check_avg_data)
    }
    max_of_phase_avg := getMaxForF64Slice(graph_check_avg_data[phase_begin_ind:endidx])
    // because the arrays should be of equal length, we don't need to recalculate endidx
    max_of_phase_max := getMaxForF64Slice(graph_check_max_data[phase_begin_ind:endidx])
    phrase_rate := rateAttr(max_of_phase_avg)
    phrase_temp := rateAttr(max_of_phase_max)
    if (phrase_temp != phrase_rate) { // give a range 'leichter bis starker' instead of just a single word
      phrase_rate = phrase_rate + " bis " + phrase_temp
    }
    way_begin_min := getIndexForCondition(raar.RainTable[phase_begin_ind], hf_xabovethres)
    way_begin_per := float64(way_begin_min) * float64(raar.TimeStep) / raar.TravelDuration;
    var phrase_way_begin string = "";
    if (len(raar.RainTable[phase_begin_ind]) > 1) { // FIXME? not sure if I interpreted the Javascript correctly there
      phrase_way_begin = wayAttr(way_begin_per)
    }
    if (phase_begin_ind == 0) { // already raining
      raar.LightColor = 1 // red
      raar.Message1 = fmt.Sprintf("Noch %d Minuten", phase_pause_ind * raar.CheckStep)
      raar.Message2 = fmt.Sprintf("bis %s Uhr %s Niederschlag %s",
                                  timeIn(phase_pause_ind * raar.CheckStep),
                                  phrase_rate,
                                  phrase_way_begin)
      if (phase_pause_ind < 0) { // kein Pause gefunden, daher open end
        raar.Message1 = fmt.Sprintf("Über %d Stunden", raar.ForecastHorizon / 60.0)
        raar.Message2 = fmt.Sprintf("%s Niederschlag %s", phrase_rate, phrase_way_begin)
      }
    } else {
      if ((phase_begin_ind * raar.CheckStep) <= int(prewarntime)) { // upstream has hardcoded this at 10!
        raar.LightColor = 2 // yellow
      }
      raar.Message1 = fmt.Sprintf("Es verbleiben %d Minuten", phase_begin_ind * raar.CheckStep)
      raar.Message2 = fmt.Sprintf("von %s bis %s Uhr %s Niederschlag",
                                  timeIn(phase_begin_ind * raar.CheckStep),
                                  timeIn(phase_pause_ind * raar.CheckStep),
                                  phrase_rate)
      if (phase_pause_ind < 0) { // kein Pause gefunden, daher open end
        raar.Message2 = fmt.Sprintf("ab %s Uhr %s Niederschlag", timeIn(phase_begin_ind * raar.CheckStep), phrase_rate)
      }
    }
  }
  
  // Now dump the enriched data
  if (dataformat == "json") {
    w.Header().Set("Content-Type", "application/json; charset=utf-8")
    w.Header().Set("Cache-Control", "public, max-age=59")
    err = json.NewEncoder(w).Encode(raar)
    if (err != nil) {
      log.Printf("[W] Failed to encode reply: %s", err.Error());
    }
  } else { // txt
    w.Header().Set("Content-Type", "text/plain; charset=utf-8")
    w.Header().Set("Cache-Control", "public, max-age=59")
    fmt.Fprintf(w, "LightColor=%d\n", raar.LightColor)
    fmt.Fprintf(w, "Message1=%s\n", raar.Message1)
    fmt.Fprintf(w, "Message2=%s\n", raar.Message2)
    // FIXME? Maybe more?
  }
}
