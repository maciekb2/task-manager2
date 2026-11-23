# Pierwsze kroki: od kliknięcia do wyjaśnienia

Uruchom `make up`, otwórz panel i zacznij od **Nauka krok po kroku**. Nie musisz
znać terminów Kubernetes. Na dole widoku jest rozwijany słownik pojęć.

Każda z dziewięciu lekcji podaje cel, pytanie do przemyślenia, jedną akcję,
oczekiwany wynik i wyjaśnienie. Zaliczenie wymaga świeżych zdarzeń lub stanu
zasobów z API, a nie samego przyjęcia polecenia czy upływu czasu. Postęp jest
zapamiętany w tej przeglądarce. Lekcję można powtórzyć.

1. **Droga żądania:** uruchom ruch i zobacz jeden identyfikator w kilku usługach.
2. **Obiekty:** Pod to kopia aplikacji, Service to adres do kopii, a Deployment
   pilnuje ich liczby. Panel odczytuje wszystkie trzy z prawdziwego klastra.
3. **Awaria poda:** usuń kopię workera i zaczekaj na nową gotową kopię.
4. **Skalowanie:** obserwuj różnicę między trzema zadanymi i trzema gotowymi podami.
5. **Kolejka:** zatrzymaj konsumenta; trzy nowe wiadomości powinny zostać w brokerze.
6. **Retry:** wznów konsumenta i zobacz nowe zadanie zakończone po dwóch próbach.
7. **Job:** uruchom pracę, która ma się poprawnie zakończyć.
8. **CronJob:** poczekaj na nowy zaplanowany Job; lekcja potem wstrzyma harmonogram.
9. **Wydajność:** uruchom pracę CPU i otwórz dashboard. Porównaj ją ze scenariuszem
   wolnej usługi, która głównie czeka.

**Przywróć scenariusz bazowy** zatrzymuje ruch, przywraca domyślne repliki i
wstrzymuje CronJob. Zachowuje historię i wyniki. Przyjęte żądania mogą jeszcze
kończyć pracę; pody też nie znikają natychmiast.

## Jak czytać Wydajność i ślady

Zacznij od zakładki **Przegląd** z wykresami. W **Etapy i pody** sprawdzisz
czasy funkcji oraz próbki CPU/RAM poszczególnych podów. **Ślady żądań** pokazują
drogę jednego żądania, a **Czas zadań** — pracę kolejki i Jobów.

* **CPU podów** to suma pomiarów dla procesów laboratorium. Jeden rdzeń to około
  1000 mCPU. Wykres węzła obejmuje również Kubernetes/control plane — dane pochodzą
  z Kubernetes Metrics API, bez monitorowania hosta stacji roboczej.
* **RAM** to próbki pamięci z tego samego API. Wybierz komponent, aby zobaczyć jego
  kopie. Metryki mają własny czas i okno pomiaru; nie są odczytem co milisekundę.
* **P95** oznacza, że 95% zmierzonych czasów jest nie większe od tej wartości.
  To nie średnia. Przy małej liczbie żądań wynik może mocno się zmieniać.
* **Funkcje i etapy** pokazują wywołania i rzeczywiście zmierzony czas ścienny.
  `echo.delay` to czekanie, `echo.cpu` to pętla obliczeń, `validator.validate` to
  walidacja. Nazwy są konkretną instrumentacją kodu, nie automatycznym profilerem
  wszystkich funkcji Go.
* **Trace** łączy spany z wielu procesów dzięki standardowemu `traceparent`.
  Wybierz żądanie lub zadanie, aby zobaczyć waterfall z Jaegera. Wcięcia pokazują
  relacje rodzic–dziecko. Spany są zagnieżdżone: nie sumuj ich jako czasu żądania.
* **Zadania** pokazują czas do pierwszego podjęcia i łączny czas do wyniku.
  Ten drugi zawiera kolejkę i oczekiwanie na retry, nie tylko pracę procesora.

CPU/RAM nie są dokładnym kosztem pojedynczego żądania. W podzie mogą działać
równocześnie różne operacje, a Metrics Server zwraca pomiar w oknie czasowym.
Porównanie eksperymentów pokazuje związek obciążenia i czasu, nie dowód precyzyjnej
atrybucji CPU do jednej funkcji. Brakujących próbek nie zastępujemy sztucznym zerem.

Historia wykresów jest ograniczona do 15 minut w pamięci kontrolera i zaczyna się
zbierać po jego starcie. Jaeger przechowuje do 5000 trace w pamięci; jego restart
usuwa ślady. Eksport spanów jest partiami — świeży trace może pojawić się po chwili.
Wyniki zadań pozostają w osobnym KV JetStream zgodnie z dotychczasową retencją.
